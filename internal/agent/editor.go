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
	"syscall"
	"time"

	"github.com/shleesauce/lattice/internal/proto"
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

// release stops the code-server process AND its descendants. Idempotent.
//
// ctx cancel only kills the direct child; code-server forks node worker /
// extension-host children that survive and leak processes + bound loopback ports
// across create/close cycles. killProcessGroup signals the whole group (FIX 1).
// We still cancel the ctx so the Cmd's own waiter unwinds and ProcessState fills.
func (s *editorSession) release() {
	s.closeOnce.Do(func() {
		if s.pid > 0 {
			killProcessGroup(s.pid)
		}
		if s.cancel != nil {
			s.cancel()
		}
	})
}

// editorSessions is the process-global registry of live code-server processes,
// keyed by sessionId. Mirrors terminals; NOT torn down on the
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

	// Seed a per-session VS Code settings.json so code-server boots already wearing
	// the Lattice skin (theme via colorCustomizations — no extension to install) and
	// without the stock welcome/menu clutter. Best-effort: a seed failure just means
	// a plainer editor, never a failed session.
	udd := filepath.Join(editorStateBaseDir(), p.SessionID)
	if err := seedEditorSettings(udd); err != nil {
		log.Printf("agent: editor %s: seed settings: %v (continuing)", p.SessionID, err)
	}

	ctx, cancel := context.WithCancel(e.baseCtx)
	cmd := exec.CommandContext(ctx, bin, codeServerArgs(addr, p.Cwd, udd)...)
	if p.Cwd != "" {
		cmd.Dir = p.Cwd
	}
	cmd.Env = os.Environ()
	// Own process group so the whole code-server subtree (node workers /
	// extension hosts) can be torn down together in release() (FIX 1).
	cmd.SysProcAttr = &syscall.SysProcAttr{}
	setProcGroup(cmd.SysProcAttr)

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
func codeServerArgs(addr, cwd, udd string) []string {
	args := []string{
		"--bind-addr", addr,
		"--auth", "none",
		"--trusted-origins", "*",
		"--app-name", "Lattice", // brands the title/welcome as Lattice, not code-server
		"--disable-telemetry",
		"--disable-update-check",
		"--disable-workspace-trust",
		"--user-data-dir", udd,
	}
	if ext := sharedExtensionsDir(); ext != "" {
		args = append(args, "--extensions-dir", ext)
	}
	if cwd != "" {
		args = append(args, cwd)
	}
	return args
}

// editorSettingsJSON is the seeded VS Code settings for every editor session. It
// makes code-server wear the Lattice "Cool Fabric, Warm Life" skin via workbench
// colorCustomizations + tokenColorCustomizations (no theme extension required):
// true-black editor, cool structure, WARM caret/active-tab/selection ("where you
// are working"), green run/progress, calm cool syntax with warm only on literals.
// It also strips the stock welcome/walkthrough and points fonts at IBM Plex Mono.
//
// De-chrome (F12): the editor lives INSIDE Lattice's own pane next to the Lattice
// Claude split, so code-server's full IDE chrome is redundant — it reads as an
// "app inside an app." We hide the activity-bar icon rail, the menu bar, the
// layout-control widget, and keep the built-in chat/agent auxiliary panel closed
// (Lattice's Claude is the SOLE AI surface). The file tree (Explorer side bar),
// open/edit/save, git decorations, and the status bar all remain functional —
// only the redundant navigation chrome is removed. Enum values verified against
// the bundled code-server 1.112 schema (activityBar.location accepts "hidden";
// secondarySideBar.defaultVisibility accepts "hidden").
const editorSettingsJSON = `{
  "workbench.colorTheme": "Default Dark Modern",
  "workbench.startupEditor": "none",
  "workbench.tips.enabled": false,
  "workbench.welcomePage.walkthroughs.openOnInstall": false,
  "workbench.activityBar.location": "hidden",
  "workbench.layoutControl.enabled": false,
  "workbench.secondarySideBar.defaultVisibility": "hidden",
  "window.menuBarVisibility": "hidden",
  "window.commandCenter": false,
  "editor.fontFamily": "'IBM Plex Mono', ui-monospace, monospace",
  "editor.fontSize": 13,
  "editor.fontLigatures": false,
  "editor.minimap.enabled": false,
  "editor.renderWhitespace": "none",
  "editor.cursorBlinking": "smooth",
  "terminal.integrated.fontFamily": "'IBM Plex Mono', monospace",
  "terminal.integrated.fontSize": 12.5,
  "telemetry.telemetryLevel": "off",
  "update.mode": "none",
  "editor.tokenColorCustomizations": {
    "comments": "#6E7B84",
    "keywords": "#38BDF8",
    "strings": "#F5A623",
    "numbers": "#FFC24B",
    "functions": "#2DE2C0",
    "types": "#5BD6F0",
    "variables": "#E9EFF1"
  },
  "workbench.colorCustomizations": {
    "editor.background": "#000000",
    "editor.foreground": "#E9EFF1",
    "editorCursor.foreground": "#F5A623",
    "editor.selectionBackground": "#F5A62330",
    "editor.lineHighlightBackground": "#F5A62310",
    "editor.lineHighlightBorder": "#00000000",
    "editorLineNumber.foreground": "#3A444C",
    "editorLineNumber.activeForeground": "#A4B0B8",
    "editorWhitespace.foreground": "#1A2228",
    "editorIndentGuide.background1": "#141A20",
    "foreground": "#A4B0B8",
    "focusBorder": "#2DE2C0",
    "sideBar.background": "#07090C",
    "sideBar.foreground": "#A4B0B8",
    "sideBar.border": "#171D25",
    "sideBarTitle.foreground": "#6E7B84",
    "sideBarSectionHeader.background": "#07090C",
    "activityBar.background": "#000000",
    "activityBar.foreground": "#2DE2C0",
    "activityBar.inactiveForeground": "#6E7B84",
    "activityBar.border": "#171D25",
    "activityBarBadge.background": "#2DE2C0",
    "activityBarBadge.foreground": "#04130F",
    "editorGroupHeader.tabsBackground": "#07090C",
    "editorGroupHeader.border": "#171D25",
    "tab.activeBackground": "#000000",
    "tab.inactiveBackground": "#07090C",
    "tab.activeForeground": "#E9EFF1",
    "tab.inactiveForeground": "#6E7B84",
    "tab.activeBorderTop": "#F5A623",
    "tab.border": "#171D25",
    "tab.activeBorder": "#00000000",
    "titleBar.activeBackground": "#07090C",
    "titleBar.activeForeground": "#A4B0B8",
    "titleBar.border": "#171D25",
    "statusBar.background": "#07090C",
    "statusBar.foreground": "#6E7B84",
    "statusBar.border": "#171D25",
    "statusBar.noFolderBackground": "#07090C",
    "statusBarItem.remoteBackground": "#0E1218",
    "statusBarItem.remoteForeground": "#2DE2C0",
    "panel.background": "#000000",
    "panel.border": "#171D25",
    "panelTitle.activeForeground": "#E9EFF1",
    "panelTitle.inactiveForeground": "#6E7B84",
    "terminal.background": "#000000",
    "terminal.foreground": "#A4B0B8",
    "terminalCursor.foreground": "#F5A623",
    "editorWidget.background": "#0E1218",
    "editorWidget.border": "#171D25",
    "input.background": "#000000",
    "input.border": "#171D25",
    "input.foreground": "#E9EFF1",
    "dropdown.background": "#0E1218",
    "dropdown.border": "#171D25",
    "list.activeSelectionBackground": "#171D25",
    "list.activeSelectionForeground": "#E9EFF1",
    "list.inactiveSelectionBackground": "#0E1218",
    "list.hoverBackground": "#0E1218",
    "list.focusOutline": "#2DE2C0",
    "button.background": "#2DE2C0",
    "button.foreground": "#04130F",
    "button.hoverBackground": "#5BF0D4",
    "badge.background": "#2DE2C0",
    "badge.foreground": "#04130F",
    "progressBar.background": "#2DE2C0",
    "scrollbarSlider.background": "#1A222880",
    "scrollbarSlider.hoverBackground": "#27313880",
    "scrollbarSlider.activeBackground": "#2DE2C080",
    "minimap.background": "#000000",
    "widget.border": "#171D25",
    "gitDecoration.modifiedResourceForeground": "#F5A623",
    "gitDecoration.untrackedResourceForeground": "#2FD98A",
    "gitDecoration.deletedResourceForeground": "#FF5C6C"
  }
}
`

// seedEditorSettings writes the Lattice settings.json into a session's user-data
// dir (<udd>/User/settings.json), creating the dir tree. Overwrites each launch
// so the skin stays applied.
func seedEditorSettings(udd string) error {
	userDir := filepath.Join(udd, "User")
	if err := os.MkdirAll(userDir, 0o755); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(userDir, "settings.json"), []byte(editorSettingsJSON), 0o644)
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
