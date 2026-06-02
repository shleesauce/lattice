package agent

import (
	"context"
	"encoding/base64"
	"io"
	"log"
	"os"
	"os/exec"
	"runtime"
	"sync"
	"sync/atomic"
	"time"

	pty "github.com/aymanbagabas/go-pty"

	"github.com/dylanstoryyy/lattice/internal/proto"
)

// ptyReadChunk is the read size for streaming PTY output back to the hub. Small
// enough to feel interactive, large enough to avoid frame spam.
const ptyReadChunk = 4096

// ptySession is one live interactive shell attached to a pseudo-terminal. Phase
// 3 (D18): the process is keyed by sessionId and OUTLIVES the browser/hub link.
// pumpOutput writes raw bytes to ring (always) + the swappable sink (when a
// connection is live), so a re-attaching browser is replayed the scrollback.
type ptySession struct {
	id        string
	kind      proto.SessionKind
	cwd       string
	startedAt time.Time
	pid       int

	pty    pty.Pty
	cmd    *pty.Cmd
	cancel context.CancelFunc

	ring *byteRing

	// explicitClose marks that the hub asked to close this session, so the
	// waiter goroutine suppresses its own session_exit (the close path emits it).
	explicitClose atomic.Bool
	closeOnce     sync.Once
}

// release tears down the PTY and signals the owning command to stop. Idempotent.
func (s *ptySession) release() {
	s.closeOnce.Do(func() {
		if s.cancel != nil {
			s.cancel()
		}
		if s.pty != nil {
			s.pty.Close()
		}
	})
}

// terminals is the process-global registry of live PTYs, keyed by sessionId.
// Unlike Phase 2 it is NOT torn down on disconnect — sessions survive reconnect.
type terminals struct {
	mu       sync.Mutex
	sessions map[string]*ptySession
	sink     sink
	// baseCtx is the PROCESS-GLOBAL context (from Run), NOT a per-connection
	// context. PTYs are spawned from it so they survive a hub reconnect (D18) —
	// binding them to the connection context would kill every session whenever
	// the agent↔hub link drops (e.g. a hub restart).
	baseCtx context.Context
}

// newTerminals builds an empty process-global PTY registry rooted at the
// process-lifetime context.
func newTerminals(baseCtx context.Context) *terminals {
	return &terminals{sessions: make(map[string]*ptySession), baseCtx: baseCtx}
}

func (t *terminals) put(s *ptySession) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.sessions[s.id] = s
}

func (t *terminals) get(id string) (*ptySession, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	s, ok := t.sessions[id]
	return s, ok
}

func (t *terminals) remove(id string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	delete(t.sessions, id)
}

// descriptors snapshots the live PTY sessions for re-discovery (F). Both terminal
// and claude sessions live here now (D35); each reports its own kind. A claude
// session's ClaudeSessionID equals its sessionId (the hub-assigned --session-id).
func (t *terminals) descriptors() []proto.SessionDescriptor {
	t.mu.Lock()
	defer t.mu.Unlock()
	out := make([]proto.SessionDescriptor, 0, len(t.sessions))
	for _, s := range t.sessions {
		d := proto.SessionDescriptor{
			SessionID: s.id,
			Kind:      s.kind,
			Cwd:       s.cwd,
			PID:       s.pid,
			StartedAt: s.startedAt.UTC().Format(time.RFC3339),
		}
		if s.kind == proto.SessionClaude {
			d.ClaudeSessionID = s.id
		}
		out = append(out, d)
	}
	return out
}

// close marks a session for explicit teardown and releases it. Returns true if
// the session was live (so the caller can emit a single session_exit).
func (t *terminals) close(id string) bool {
	t.mu.Lock()
	s, ok := t.sessions[id]
	if ok {
		s.explicitClose.Store(true)
		delete(t.sessions, id)
	}
	t.mu.Unlock()
	if !ok {
		return false
	}
	s.release()
	return true
}

// start spawns a long-lived PTY keyed by SessionID running the OS default shell,
// streaming output to the ring + current sink. On natural shell exit it emits
// session_exit and cleans up. Returns the pid (0 on failure) and any start error.
//
// The process is rooted at the registry's PROCESS-GLOBAL baseCtx (set in Run),
// NOT at any per-connection context, so the shell survives hub reconnects (D18).
// The `parent` arg is accepted for call-site symmetry but intentionally not used
// to bound the process lifetime.
func (t *terminals) start(parent context.Context, p proto.SessionCreatePayload) (int, error) {
	_ = parent
	if _, exists := t.get(p.SessionID); exists {
		// Already live (e.g. duplicate create): treat as success, idempotent.
		s, _ := t.get(p.SessionID)
		return s.pid, nil
	}

	pt, err := pty.New()
	if err != nil {
		return 0, err
	}

	// Branch the command on kind: a claude session is an INTERACTIVE claude in this
	// same PTY (D35), a terminal is the OS shell. The claude launch also runs under
	// the scrubbed child env so it uses the Max subscription (OAuth), never an API
	// key — both correct and cost-critical.
	kind := p.Kind
	if kind == "" {
		kind = proto.SessionTerminal
	}
	var name string
	var args []string
	ctx, cancel := context.WithCancel(t.baseCtx)
	if kind == proto.SessionClaude {
		name, args = claudeCommand(p)
	} else {
		name, args = shellForTerminal()
	}
	cmd := pt.CommandContext(ctx, name, args...)
	if p.Cwd != "" {
		cmd.Dir = p.Cwd
	}
	if kind == proto.SessionClaude {
		cmd.Env = claudeChildEnv()
	}

	cols, rows := normalizeWinsize(p.Cols, p.Rows)
	if err := pt.Resize(int(cols), int(rows)); err != nil {
		log.Printf("agent: term %s initial resize: %v", p.SessionID, err)
	}

	if err := cmd.Start(); err != nil {
		cancel()
		pt.Close()
		return 0, err
	}

	sess := &ptySession{
		id:        p.SessionID,
		kind:      kind,
		cwd:       p.Cwd,
		startedAt: time.Now(),
		pty:       pt,
		cmd:       cmd,
		cancel:    cancel,
		ring:      newByteRing(proto.TermRingBytes),
	}
	if cmd.Process != nil {
		sess.pid = cmd.Process.Pid
	}
	t.put(sess)

	go t.pumpOutput(sess)

	go func() {
		waitErr := cmd.Wait()
		exitCode := 0
		if waitErr != nil {
			if cmd.ProcessState != nil {
				exitCode = cmd.ProcessState.ExitCode()
			} else {
				exitCode = -1
			}
		}
		sess.release()
		t.remove(sess.id)
		// If the hub explicitly closed this session, the close path already
		// emitted session_exit — don't double-send.
		if !sess.explicitClose.Load() {
			t.sendExit(sess.id, exitCode, "")
		}
	}()

	return sess.pid, nil
}

// pumpOutput reads the PTY and writes raw bytes to the scrollback ring (always)
// and a base64 term_output frame to the current sink (when a connection is live)
// until the PTY closes or the session ctx is cancelled.
func (t *terminals) pumpOutput(sess *ptySession) {
	buf := make([]byte, ptyReadChunk)
	for {
		n, err := sess.pty.Read(buf)
		if n > 0 {
			sess.ring.write(buf[:n])
			frame, encErr := proto.Encode(proto.TypeTermOutput, proto.TermDataPayload{
				TermID: sess.id,
				Data:   base64.StdEncoding.EncodeToString(buf[:n]),
			})
			if encErr != nil {
				log.Printf("agent: encode term_output: %v", encErr)
			} else {
				t.sink.send(frame)
			}
		}
		if err != nil {
			if err != io.EOF {
				log.Printf("agent: term %s read: %v", sess.id, err)
			}
			return
		}
	}
}

// attach answers session_attach for a terminal: refit the window and reply with
// a session_replay carrying the base64 scrollback snapshot.
func (t *terminals) attach(p proto.SessionAttachPayload) (proto.SessionReplayPayload, bool) {
	sess, ok := t.get(p.SessionID)
	if !ok {
		return proto.SessionReplayPayload{}, false
	}
	if p.Cols != 0 || p.Rows != 0 {
		cols, rows := normalizeWinsize(p.Cols, p.Rows)
		if err := sess.pty.Resize(int(cols), int(rows)); err != nil {
			log.Printf("agent: term %s attach resize: %v", p.SessionID, err)
		}
	}
	data, truncated := sess.ring.snapshot()
	return proto.SessionReplayPayload{
		SessionID: p.SessionID,
		Kind:      sess.kind,
		Data:      base64.StdEncoding.EncodeToString(data),
		Truncated: truncated,
	}, true
}

// input writes decoded keystrokes to a live PTY.
func (t *terminals) input(p proto.TermDataPayload) {
	sess, ok := t.get(p.TermID)
	if !ok {
		return
	}
	data, err := base64.StdEncoding.DecodeString(p.Data)
	if err != nil {
		log.Printf("agent: term %s input decode: %v", p.TermID, err)
		return
	}
	if _, err := sess.pty.Write(data); err != nil {
		log.Printf("agent: term %s write: %v", p.TermID, err)
	}
}

// resize resizes a live PTY's window.
func (t *terminals) resize(p proto.TermResizePayload) {
	sess, ok := t.get(p.TermID)
	if !ok {
		return
	}
	cols, rows := normalizeWinsize(p.Cols, p.Rows)
	if err := sess.pty.Resize(int(cols), int(rows)); err != nil {
		log.Printf("agent: term %s resize: %v", p.TermID, err)
	}
}

// sendExit emits a session_exit frame for the given terminal session.
func (t *terminals) sendExit(sessionID string, code int, errMsg string) {
	frame, err := proto.Encode(proto.TypeSessionExit, proto.SessionControlPayload{
		SessionID: sessionID,
		ExitCode:  code,
		Error:     errMsg,
	})
	if err != nil {
		log.Printf("agent: encode session_exit: %v", err)
		return
	}
	t.sink.send(frame)
}

// sendLegacyExit emits a Phase-2 term_exit frame, used only by the back-compat
// /ws/terminal path which still listens for term_exit (not session_exit).
func (t *terminals) sendLegacyExit(ctx context.Context, outbound chan<- []byte, termID string, code int, errMsg string) {
	frame, err := proto.Encode(proto.TypeTermExit, proto.TermControlPayload{
		TermID:   termID,
		ExitCode: code,
		Error:    errMsg,
	})
	if err != nil {
		log.Printf("agent: encode term_exit: %v", err)
		return
	}
	select {
	case outbound <- frame:
	case <-ctx.Done():
	}
}

// normalizeWinsize clamps zero dimensions to sane defaults.
func normalizeWinsize(cols, rows uint16) (uint16, uint16) {
	if cols == 0 {
		cols = 80
	}
	if rows == 0 {
		rows = 24
	}
	return cols, rows
}

// GUARDRAIL (D35): NEVER add -p/--print/--output-format stream-json here. Headless mode bills against the separate Agent SDK credit pool (June 15 2026+). The Claude tab is INTENTIONALLY an interactive claude in a PTY. Do not reintroduce headless mode unless Dylan explicitly authorizes it.
//
// claudeCommand builds the argv for an INTERACTIVE claude launched in this PTY.
// The hub assigns --session-id so the Lattice sessionId IS the claude session id;
// --resume reattaches a prior conversation from the Syncthing-synced transcript
// (D20). bypassPermissions is always on (D35 supersedes the D21 approval mode).
func claudeCommand(p proto.SessionCreatePayload) (name string, args []string) {
	name = resolveClaude()
	if name == "" {
		// Fall back to the bare name; exec will surface a clear not-found error
		// (placement should have excluded this agent, so this is defensive).
		name = "claude"
	}
	args = []string{
		"--session-id", p.SessionID,
		"--permission-mode", "bypassPermissions",
	}
	if p.ResumeID != "" {
		args = append(args, "--resume", p.ResumeID)
	}
	return name, args
}

// shellForTerminal picks the OS default interactive shell + args.
func shellForTerminal() (string, []string) {
	if runtime.GOOS == "windows" {
		if ps := lookupWindowsShell(); ps != "" {
			return ps, nil
		}
		return "cmd.exe", nil
	}
	if sh := os.Getenv("SHELL"); sh != "" {
		return sh, nil
	}
	for _, candidate := range []string{"/bin/zsh", "/bin/bash", "/bin/sh"} {
		if _, err := os.Stat(candidate); err == nil {
			return candidate, nil
		}
	}
	return "/bin/sh", nil
}

// lookupWindowsShell prefers powershell.exe, falling back to "" so the caller
// uses cmd.exe.
func lookupWindowsShell() string {
	for _, candidate := range []string{"powershell.exe", "pwsh.exe"} {
		if _, err := exec.LookPath(candidate); err == nil {
			return candidate
		}
	}
	return ""
}
