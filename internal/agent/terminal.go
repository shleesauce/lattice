package agent

import (
	"context"
	"encoding/base64"
	"io"
	"log"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	pty "github.com/aymanbagabas/go-pty"

	"github.com/shleesauce/lattice/internal/proto"
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

	// writeMu serialises everything that writes to the pty — user input, the
	// seed-injection goroutine, and resize. go-pty does not guarantee
	// concurrent-write safety, and these three run on independent goroutines
	// (FIX 7).
	writeMu sync.Mutex

	ring *byteRing

	// lastOutNano is the unix-nano timestamp of the most recent PTY output, used by
	// the seed-injection readiness check (wait for the TUI to stop rendering).
	lastOutNano atomic.Int64

	// explicitClose marks that the hub asked to close this session, so the
	// waiter goroutine suppresses its own session_exit (the close path emits it).
	explicitClose atomic.Bool
	closeOnce     sync.Once
}

// release tears down the PTY and signals the owning command — AND its whole
// descendant tree — to stop. Idempotent.
//
// ctx cancel / pty.Close only reaches the direct child; an interactive claude
// (launched via `sh -l -c "...exec claude..."`) spawns MCP servers / hooks that
// would otherwise survive and leak processes + bound loopback ports across
// create/close cycles. go-pty starts the child with Setsid, so its pid IS its
// process-group id — killProcessGroup(pid) reaps the entire subtree (FIX 1).
func (s *ptySession) release() {
	s.closeOnce.Do(func() {
		if s.pid > 0 {
			killProcessGroup(s.pid)
		}
		if s.cancel != nil {
			s.cancel()
		}
		if s.pty != nil {
			s.pty.Close()
		}
	})
}

// write serialises a write to the pty (FIX 7): input, seed injection, and
// resize all share one pty with no concurrency guarantee from go-pty.
func (s *ptySession) write(b []byte) (int, error) {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	return s.pty.Write(b)
}

// resizeWindow serialises a resize under the same mutex as writes, since a
// resize mutates the same pty the writers touch (FIX 7).
func (s *ptySession) resizeWindow(cols, rows int) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	return s.pty.Resize(cols, rows)
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
		// C (v0.1.5): inject the per-session hook env so the static --settings hook
		// script can curl the precise-state callback. Appended AFTER claudeChildEnv so
		// these win; empty (no-op) when the hub didn't wire hooks (no HubURL/token).
		cmd.Env = append(cmd.Env, hookEnv(p.HubURL, p.SessionID, p.HookToken)...)
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

	// Fire-and-forget (v0.1.5): a claude session that needs the operator (turn done
	// or blocked on a permission prompt) should ping the phone.
	//   - When the hub wired Claude Code hooks (HubURL+HookToken present, C), the
	//     hooks report PRECISE Stop / permission_prompt / SessionEnd edges straight
	//     to the hub — so we DON'T also run the coarse 45s PTY-quiet watcher; the
	//     hook signal replaces it (else the two would double-fire).
	//   - Without hooks (no hub URL configured), fall back to the PTY-quiet idle
	//     heuristic. Terminals/editors are excluded either way: a shell at a prompt
	//     is the normal resting state.
	hooksWired := p.HubURL != "" && p.HookToken != ""
	if kind == proto.SessionClaude && !hooksWired {
		go t.watchIdle(sess)
	}

	if p.SeedInput != "" {
		go injectSeedWhenReady(sess, p.SeedInput)
	}

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
			sess.lastOutNano.Store(time.Now().UnixNano())
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
		if err := sess.resizeWindow(int(cols), int(rows)); err != nil {
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
	if _, err := sess.write(data); err != nil {
		log.Printf("agent: term %s write: %v", p.TermID, err)
	}
}

// injectSeedWhenReady types seed (+CR) into a freshly-started session ONCE its
// interactive TUI has settled: it waits for the first PTY output, then for a brief
// quiet gap (the render finishing), with a hard ceiling so a silent process still
// gets seeded. Far more robust than a fixed sleep on a slow/cold/remote box, where
// early keystrokes are dropped before the prompt is ready. Used for the onboarding
// brief (D25/D35).
func injectSeedWhenReady(sess *ptySession, seed string) {
	const (
		quiet   = 700 * time.Millisecond // no new output for this long ⇒ TUI settled
		ceiling = 15 * time.Second       // hard cap: seed even if output never goes quiet
		poll    = 100 * time.Millisecond
	)
	deadline := time.Now().Add(ceiling)
	for time.Now().Before(deadline) {
		last := sess.lastOutNano.Load()
		if last != 0 && time.Since(time.Unix(0, last)) >= quiet {
			break // saw output and it has gone quiet → ready for input
		}
		time.Sleep(poll)
	}
	if _, err := sess.write([]byte(seed + "\r")); err != nil {
		log.Printf("agent: seed %s write: %v", sess.id, err)
	}
}

// idleThreshold is how long a claude PTY must stay quiet before the agent reports
// it idle. Override with LATTICE_IDLE_SECS (clamped to a sane floor) — a fleet that
// runs slow tasks may want a longer fuse to avoid mid-thought pings.
func idleThreshold() time.Duration {
	const def = 45 * time.Second
	v := os.Getenv("LATTICE_IDLE_SECS")
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(strings.TrimSpace(v))
	if err != nil || n < 5 {
		return def
	}
	return time.Duration(n) * time.Second
}

// watchIdle reports the quiet→active edges of a claude session to the hub. It
// edge-triggers: one session_idle{Idle:true} when output has been silent for the
// threshold, one session_idle{Idle:false} when output resumes — so the hub gets a
// single "needs you" signal per waiting episode, not a stream. It rides the same
// lastOutNano the seed-injector watches (stamped on every PTY read in pumpOutput),
// so it sees activity even with no browser attached — the whole point of fire-and-
// forget. The loop exits when the session leaves the registry (process ended).
func (t *terminals) watchIdle(sess *ptySession) {
	const poll = 3 * time.Second
	threshold := idleThreshold()
	notified := false
	for {
		time.Sleep(poll)
		if _, ok := t.get(sess.id); !ok {
			return // session ended (the wait goroutine removed it)
		}
		last := sess.lastOutNano.Load()
		if last == 0 {
			continue // no output yet — still booting, not "idle"
		}
		quiet := time.Since(time.Unix(0, last))
		switch {
		case quiet >= threshold && !notified:
			t.sendIdle(sess.id, true, quiet.Milliseconds())
			notified = true
		case quiet < threshold && notified:
			t.sendIdle(sess.id, false, 0)
			notified = false
		}
	}
}

// sendIdle emits a session_idle frame to the hub via the shared sink (the live
// agent→hub link). Dropped silently when no link is up; the next poll re-evaluates.
func (t *terminals) sendIdle(sessionID string, idle bool, quietMs int64) {
	frame, err := proto.Encode(proto.TypeSessionIdle, proto.SessionIdlePayload{
		SessionID: sessionID,
		Idle:      idle,
		QuietMs:   quietMs,
	})
	if err != nil {
		log.Printf("agent: encode session_idle: %v", err)
		return
	}
	t.sink.send(frame)
}

// resize resizes a live PTY's window.
func (t *terminals) resize(p proto.TermResizePayload) {
	sess, ok := t.get(p.TermID)
	if !ok {
		return
	}
	cols, rows := normalizeWinsize(p.Cols, p.Rows)
	if err := sess.resizeWindow(int(cols), int(rows)); err != nil {
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
	sendFrame(ctx, outbound, proto.TypeTermExit, proto.TermControlPayload{
		TermID:   termID,
		ExitCode: code,
		Error:    errMsg,
	})
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

// GUARDRAIL (D35): NEVER add -p/--print/--output-format stream-json here. Headless mode bills against the separate Agent SDK credit pool (June 15 2026+). The Claude tab is INTENTIONALLY an interactive claude in a PTY. Do not reintroduce headless mode unless the maintainer explicitly authorizes it.
//
// claudePermissionModes is the set of values claude's --permission-mode accepts.
// Anything outside it falls back to the Lattice default so a bad/empty value can
// never reach the launch.
var claudePermissionModes = map[string]bool{
	"default": true, "acceptEdits": true, "plan": true,
	"auto": true, "bypassPermissions": true, "dontAsk": true,
}

// permissionMode validates an operator-chosen mode, defaulting to bypassPermissions
// (D35 — Lattice Claude sessions run in a browser and are often unattended, so the
// no-prompt default keeps them from blocking; the dashboard lets you pick another).
func permissionMode(m string) string {
	if claudePermissionModes[m] {
		return m
	}
	return "bypassPermissions"
}

// claudeModels is the allow-list of --model values Lattice will pass through. An
// operator-chosen model must be on this list or it's dropped (claudeModel returns
// ""), so a bad/spoofed value can never reach the launch — claude then falls back
// to its own configured default. The 1M-context variants use the `[1m]` suffix
// form (verified accepted by `claude --model … --help` on claude 2.1.137); the
// `[1m]` is a literal part of the model string, NOT a placement filter. Legacy ids
// are kept so a resumed conversation pinned to an older model still relaunches.
var claudeModels = map[string]bool{
	"claude-fable-5":      true,
	"claude-fable-5[1m]":  true,
	"claude-opus-4-8":     true,
	"claude-opus-4-8[1m]": true, // Lattice default (Opus 4.8, 1M context)
	"claude-sonnet-4-6":   true,
	"claude-haiku-4-5":    true,
	"claude-opus-4-7":     true,
	"claude-opus-4-7[1m]": true,
	"claude-opus-4-6":     true,
}

// claudeModel validates an operator-chosen model id against the allow-list. An
// empty or unrecognised value returns "" — the caller then omits --model entirely,
// leaving claude on its own configured default rather than launching with a bogus
// model string.
func claudeModel(m string) string {
	m = strings.TrimSpace(m)
	if claudeModels[m] {
		return m
	}
	return ""
}

// claudeCommand builds the argv for an INTERACTIVE claude launched in this PTY.
// The hub assigns --session-id so the Lattice sessionId IS the claude session id;
// --resume reattaches a prior conversation from the Syncthing-synced transcript
// (D20). --permission-mode defaults to bypassPermissions (D35) but is per-session
// selectable from the dashboard (proto.SessionCreatePayload.PermissionMode).
func claudeCommand(p proto.SessionCreatePayload) (name string, args []string) {
	claude := resolveClaude()
	if claude == "" {
		// Fall back to the bare name; exec will surface a clear not-found error
		// (placement should have excluded this agent, so this is defensive).
		claude = "claude"
	}

	// Resume vs fresh. claude REJECTS `--session-id` together with `--resume`
	// ("can only be used with --continue or --resume if --fork-session is also
	// specified"), and --fork-session would mint a NEW id and break the one-logical-
	// identity guarantee (D20/D32). So on resume pass ONLY `--resume` (it already
	// pins the conversation id); on a fresh start pass `--session-id` so the Lattice
	// sessionId IS the claude session id.
	mode := permissionMode(p.PermissionMode)
	var cArgs []string
	if p.ResumeID != "" {
		cArgs = []string{"--resume", p.ResumeID, "--permission-mode", mode}
	} else {
		cArgs = []string{"--session-id", p.SessionID, "--permission-mode", mode}
	}
	// Operator-chosen model (validated against the allow-list). Empty ⇒ omit
	// --model so claude keeps its own default; a recognised id (incl. the `[1m]`
	// 1M-context form) is passed through verbatim.
	if model := claudeModel(p.Model); model != "" {
		cArgs = append(cArgs, "--model", model)
	}
	// Fast mode ⇒ claude's low-effort setting (verified flag on claude 2.1.137).
	if p.FastMode {
		cArgs = append(cArgs, "--effort", "low")
	}
	// C (v0.1.5): wire Lattice-managed CC hooks for precise state, but ONLY when the
	// hub shipped a callback URL (HubURL) AND we can write the static settings file.
	// `--settings` loads our hooks for THIS launch only — it never touches the user's
	// ~/.claude/settings.json. The per-session token/url/id are injected via cmd.Env
	// in start() (hookEnv), so one static settings file serves every session.
	if p.HubURL != "" && p.HookToken != "" {
		if sp := hookSettingsPath(); sp != "" {
			cArgs = append(cArgs, "--settings", sp)
		}
	}

	// Windows: exec claude.exe directly — its PATH is fine and there's no login-shell
	// convention.
	if runtime.GOOS == "windows" {
		return claude, cArgs
	}

	// Unix: launch through a LOGIN shell so claude inherits the user's REAL PATH
	// (homebrew / nvm / ~/.local/bin). The agent runs under launchd/systemd with a
	// minimal PATH, so a bare exec can't find `node` etc. — the user's SessionStart
	// hooks then fail with "node: command not found". The login shell sources the
	// user's profile first, then execs claude in this same PTY.
	sh, _ := shellForTerminal()
	var b strings.Builder
	if p.Cwd != "" {
		b.WriteString("cd ")
		b.WriteString(shQuote(p.Cwd))
		b.WriteString(" 2>/dev/null; ")
	}
	// Guarantee the common tool dirs are on PATH even when the user's LOGIN profile
	// doesn't add them — homebrew's PATH line typically lives in .zshrc (interactive),
	// which a login shell skips, so `node` (and the user's SessionStart hooks) would
	// otherwise be missing. This runs AFTER the profile, so it resolves deterministically.
	b.WriteString(`export PATH="/opt/homebrew/bin:/opt/homebrew/sbin:$HOME/.local/bin:/usr/local/bin:$PATH"; `)
	b.WriteString("exec ")
	b.WriteString(shQuote(claude))
	for _, a := range cArgs {
		b.WriteByte(' ')
		b.WriteString(shQuote(a))
	}
	return sh, []string{"-l", "-c", b.String()}
}

// shQuote single-quotes a string for safe interpolation into a POSIX shell
// command line (wrap in '…', escaping any embedded single quote as '\”).
func shQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
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
