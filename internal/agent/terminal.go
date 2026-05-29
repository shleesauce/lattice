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

	pty "github.com/aymanbagabas/go-pty"

	"github.com/dylanstoryyy/lattice/internal/proto"
)

// ptyReadChunk is the read size for streaming PTY output back to the hub. Small
// enough to feel interactive, large enough to avoid frame spam.
const ptyReadChunk = 4096

// ptySession is one live interactive shell attached to a pseudo-terminal.
type ptySession struct {
	id     string
	pty    pty.Pty
	cmd    *pty.Cmd
	cancel context.CancelFunc

	// explicitClose marks that the hub asked to close this session, so the
	// waiter goroutine suppresses its own term_exit (the close path emits it).
	// Atomic: written by close()/closeAll() and read by the waiter goroutine.
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

// terminals is the per-session registry of live PTYs, keyed by termId. It is
// owned by one agent connection and torn down when that connection ends.
type terminals struct {
	mu       sync.Mutex
	sessions map[string]*ptySession
}

// newTerminals builds an empty per-session PTY registry.
func newTerminals() *terminals {
	return &terminals{sessions: make(map[string]*ptySession)}
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

// closeAll tears down every live session — used on connection end so a hub
// reconnect does not leave orphaned shells running.
func (t *terminals) closeAll() {
	t.mu.Lock()
	live := make([]*ptySession, 0, len(t.sessions))
	for _, s := range t.sessions {
		s.explicitClose.Store(true)
		live = append(live, s)
	}
	t.sessions = make(map[string]*ptySession)
	t.mu.Unlock()
	for _, s := range live {
		s.release()
	}
}

// close marks a session for explicit teardown and releases it. Returns true if
// the session was live (so the caller can emit a single term_exit).
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

// startTerm spawns a PTY running the OS default shell and streams its output
// back as term_output frames. On natural shell exit it emits term_exit and
// cleans up. The reader and waiter run as goroutines; this call returns fast.
func (t *terminals) startTerm(parent context.Context, p proto.TermStartPayload, outbound chan<- []byte) {
	if _, exists := t.get(p.TermID); exists {
		log.Printf("agent: term_start ignored, termId %s already live", p.TermID)
		return
	}

	pt, err := pty.New()
	if err != nil {
		t.sendExit(parent, outbound, p.TermID, -1, err.Error())
		return
	}

	name, args := shellForTerminal()
	ctx, cancel := context.WithCancel(parent)
	cmd := pt.CommandContext(ctx, name, args...)

	cols, rows := normalizeWinsize(p.Cols, p.Rows)
	if err := pt.Resize(int(cols), int(rows)); err != nil {
		log.Printf("agent: term %s initial resize: %v", p.TermID, err)
	}

	if err := cmd.Start(); err != nil {
		cancel()
		pt.Close()
		t.sendExit(parent, outbound, p.TermID, -1, err.Error())
		return
	}

	sess := &ptySession{id: p.TermID, pty: pt, cmd: cmd, cancel: cancel}
	t.put(sess)

	go t.pumpOutput(ctx, sess, outbound)

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
		// emitted term_exit — don't double-send.
		if !sess.explicitClose.Load() {
			t.sendExit(parent, outbound, sess.id, exitCode, "")
		}
	}()
}

// pumpOutput reads the PTY and pushes base64 term_output frames until the PTY
// closes (shell exit) or the session ctx is cancelled.
func (t *terminals) pumpOutput(ctx context.Context, sess *ptySession, outbound chan<- []byte) {
	buf := make([]byte, ptyReadChunk)
	for {
		n, err := sess.pty.Read(buf)
		if n > 0 {
			frame, encErr := proto.Encode(proto.TypeTermOutput, proto.TermDataPayload{
				TermID: sess.id,
				Data:   base64.StdEncoding.EncodeToString(buf[:n]),
			})
			if encErr != nil {
				log.Printf("agent: encode term_output: %v", encErr)
			} else {
				select {
				case outbound <- frame:
				case <-ctx.Done():
					return
				}
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

// sendExit emits a term_exit frame for the given session.
func (t *terminals) sendExit(ctx context.Context, outbound chan<- []byte, termID string, code int, errMsg string) {
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
