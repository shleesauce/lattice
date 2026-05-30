package agent

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"github.com/dylanstoryyy/lattice/internal/proto"
)

// editorReadyTimeout bounds how long start() waits for code-server to bind its
// port before acking the session. It MUST stay under the hub's roundTrip
// pendingTimeout (10s) so the session_created ack arrives in time; code-server
// binds its HTTP port within ~1s, so this is generous headroom.
const editorReadyTimeout = 6 * time.Second

// editorSession is one live code-server process bound to a loopback port (D27).
// The hub reaches it ONLY through the yamux tunnel (no inbound listener on the
// agent's external interface — D2 preserved). Like the other session kinds it is
// keyed by sessionId and OUTLIVES the browser/hub link (D18).
type editorSession struct {
	id        string
	cwd       string
	addr      string // 127.0.0.1:<port> — the tunnel Accept loop dials this
	port      int
	startedAt time.Time
	pid       int

	cmd    *exec.Cmd
	cancel context.CancelFunc

	explicitClose atomic.Bool
	closeOnce     sync.Once
}

// release stops the code-server process. Idempotent.
func (s *editorSession) release() {
	s.closeOnce.Do(func() {
		if s.cancel != nil {
			s.cancel()
		}
	})
}

// editorSessions is the process-global registry of live code-server processes,
// keyed by sessionId. Mirrors terminals/claudeSessions; NOT torn down on the
// agent↔hub disconnect (D18), so an editor survives a hub restart and is
// re-adopted via the post-register session list.
type editorSessions struct {
	mu       sync.Mutex
	sessions map[string]*editorSession
	sink     sink
	baseCtx  context.Context // PROCESS-GLOBAL (from Run) so editors survive reconnect
}

func newEditorSessions(baseCtx context.Context) *editorSessions {
	return &editorSessions{sessions: make(map[string]*editorSession), baseCtx: baseCtx}
}

func (e *editorSessions) put(s *editorSession) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.sessions[s.id] = s
}

func (e *editorSessions) get(id string) (*editorSession, bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	s, ok := e.sessions[id]
	return s, ok
}

func (e *editorSessions) remove(id string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	delete(e.sessions, id)
}

// addrFor returns the loopback address of a live editor session's code-server,
// used by the tunnel Accept loop to connect an incoming stream to the backend.
func (e *editorSessions) addrFor(id string) (string, bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	s, ok := e.sessions[id]
	if !ok {
		return "", false
	}
	return s.addr, true
}

// descriptors snapshots live editor sessions for re-discovery (F).
func (e *editorSessions) descriptors() []proto.SessionDescriptor {
	e.mu.Lock()
	defer e.mu.Unlock()
	out := make([]proto.SessionDescriptor, 0, len(e.sessions))
	for _, s := range e.sessions {
		out = append(out, proto.SessionDescriptor{
			SessionID: s.id,
			Kind:      proto.SessionEditor,
			Cwd:       s.cwd,
			PID:       s.pid,
			StartedAt: s.startedAt.UTC().Format(time.RFC3339),
		})
	}
	return out
}

// close marks an editor session for explicit teardown and releases it. Returns
// true if it was live (so the caller emits a single session_exit).
func (e *editorSessions) close(id string) bool {
	e.mu.Lock()
	s, ok := e.sessions[id]
	if ok {
		s.explicitClose.Store(true)
		delete(e.sessions, id)
	}
	e.mu.Unlock()
	if !ok {
		return false
	}
	s.release()
	return true
}

// attach is a no-op for editors: the browser connects to code-server directly
// through the /editor/{id}/ proxy (its own iframe), not the /ws/session bridge,
// so there is no scrollback/event tail to replay. Returns ok=false so the
// session_attach dispatcher treats it as unhandled.
func (e *editorSessions) attach(p proto.SessionAttachPayload) (proto.SessionReplayPayload, bool) {
	return proto.SessionReplayPayload{}, false
}

// start spawns code-server for the session bound to a free loopback port, waits
// (briefly) for the port to come up, and returns the pid. The process is rooted
// at the PROCESS-GLOBAL baseCtx so it survives hub reconnects (D18).
func (e *editorSessions) start(parent context.Context, p proto.SessionCreatePayload) (int, error) {
	_ = parent // lifetime is baseCtx, see D18
	if s, exists := e.get(p.SessionID); exists {
		return s.pid, nil // idempotent
	}

	bin := resolveCodeServer()
	if bin == "" {
		return 0, errors.New("code-server not installed")
	}
	port, err := freeLoopbackPort()
	if err != nil {
		return 0, err
	}
	addr := fmt.Sprintf("127.0.0.1:%d", port)

	ctx, cancel := context.WithCancel(e.baseCtx)
	cmd := exec.CommandContext(ctx, bin, codeServerArgs(p.SessionID, addr, p.Cwd)...)
	if p.Cwd != "" {
		cmd.Dir = p.Cwd
	}
	cmd.Env = os.Environ()

	stdout, _ := cmd.StdoutPipe()
	stderr, _ := cmd.StderrPipe()
	if err := cmd.Start(); err != nil {
		cancel()
		return 0, err
	}

	sess := &editorSession{
		id:        p.SessionID,
		cwd:       p.Cwd,
		addr:      addr,
		port:      port,
		startedAt: time.Now(),
		cmd:       cmd,
		cancel:    cancel,
	}
	if cmd.Process != nil {
		sess.pid = cmd.Process.Pid
	}
	e.put(sess)

	go drainEditorLog(sess.id, stdout)
	go drainEditorLog(sess.id, stderr)

	go func() {
		_ = cmd.Wait()
		sess.release()
		e.remove(sess.id)
		if !sess.explicitClose.Load() {
			code := 0
			if cmd.ProcessState != nil {
				code = cmd.ProcessState.ExitCode()
			}
			e.sendExit(sess.id, code, "")
		}
	}()

	// Wait for the HTTP port to accept before acking, so the first iframe load
	// proxies to a live backend instead of a 502. Best-effort: on timeout we ack
	// anyway (the user can reload) rather than fail the session.
	if err := waitListening(ctx, addr, editorReadyTimeout); err != nil {
		log.Printf("agent: editor %s not listening within %s: %v (acking anyway)", sess.id, editorReadyTimeout, err)
	}
	return sess.pid, nil
}

// sendExit emits a session_exit frame for an editor session.
func (e *editorSessions) sendExit(sessionID string, code int, errMsg string) {
	frame, err := proto.Encode(proto.TypeSessionExit, proto.SessionControlPayload{
		SessionID: sessionID,
		ExitCode:  code,
		Error:     errMsg,
	})
	if err != nil {
		log.Printf("agent: encode editor session_exit: %v", err)
		return
	}
	e.sink.send(frame)
}

// codeServerArgs builds the code-server argv. The recipe is the proven P1 spike:
// loopback bind, no auth (the tailnet + hub already gate access — D2/D3),
// trusted-origins="*" so code-server's origin check doesn't 403 the proxied
// WebSocket, telemetry/update/trust prompts disabled. A per-session user-data-dir
// isolates the IPC socket so concurrent editors don't collide, while extensions
// are shared so installs carry across sessions. The positional arg opens the
// project folder.
func codeServerArgs(sessionID, addr, cwd string) []string {
	args := []string{
		"--bind-addr", addr,
		"--auth", "none",
		"--trusted-origins", "*",
		"--disable-telemetry",
		"--disable-update-check",
		"--disable-workspace-trust",
		"--user-data-dir", filepath.Join(editorStateBaseDir(), sessionID),
	}
	if ext := sharedExtensionsDir(); ext != "" {
		args = append(args, "--extensions-dir", ext)
	}
	if cwd != "" {
		args = append(args, cwd)
	}
	return args
}

// editorStateBaseDir is the parent of the per-session user-data directories.
func editorStateBaseDir() string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return filepath.Join(os.TempDir(), "lattice-editor")
	}
	return filepath.Join(home, ".local", "share", "lattice-editor")
}

// sharedExtensionsDir is the single extensions directory all editor sessions
// share so a `code-server --install-extension` (or a manual install) is visible
// to every project. Empty ⇒ let code-server use its default.
func sharedExtensionsDir() string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return ""
	}
	return filepath.Join(home, ".local", "share", "code-server", "extensions")
}

// freeLoopbackPort asks the OS for an unused loopback TCP port. The brief gap
// between closing the probe listener and code-server binding is an acceptable
// race for a single-user agent.
func freeLoopbackPort() (int, error) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port, nil
}

// waitListening blocks until addr accepts a TCP connection or the deadline/ctx
// elapses.
func waitListening(ctx context.Context, addr string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		conn, err := net.DialTimeout("tcp", addr, 500*time.Millisecond)
		if err == nil {
			conn.Close()
			return nil
		}
		if time.Now().After(deadline) {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(150 * time.Millisecond):
		}
	}
}

// drainEditorLog logs code-server stdout/stderr so boot/auth failures are
// diagnosable. Bounded line buffer; code-server is not chatty after boot.
func drainEditorLog(sessionID string, r io.Reader) {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 8<<10), 1<<20)
	for scanner.Scan() {
		log.Printf("agent: editor %s: %s", sessionID, scanner.Text())
	}
}
