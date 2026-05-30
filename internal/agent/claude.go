package agent

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"log"
	"os"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/dylanstoryyy/lattice/internal/proto"
)

// claudeStdoutBuf bounds a single stream-json line. One event (e.g. a large
// tool_result or a full assistant message) can be big, so the scanner buffer is
// generous: a too-small buffer would truncate JSON and desync the stream.
const claudeStdoutBuf = 4 << 20 // 4 MiB

// claudeSession is one live `claude` process driven headless in stream-json. It
// is keyed by sessionId, OUTLIVES the browser/hub link (D18), and keeps a bounded
// tail of recent events for replay on (re)attach.
type claudeSession struct {
	id        string
	cwd       string
	claudeID  string // == id (hub assigns --session-id)
	startedAt time.Time
	pid       int

	cmd     *exec.Cmd
	stdin   io.WriteCloser
	cancel  context.CancelFunc
	stdinMu sync.Mutex

	ringMu sync.Mutex
	ring   []json.RawMessage // bounded tail (proto.ClaudeEventRingMax)

	explicitClose atomic.Bool
	closeOnce     sync.Once
}

// release stops the process. Idempotent.
func (s *claudeSession) release() {
	s.closeOnce.Do(func() {
		if s.cancel != nil {
			s.cancel()
		}
		s.stdinMu.Lock()
		if s.stdin != nil {
			s.stdin.Close()
		}
		s.stdinMu.Unlock()
	})
}

// appendEvent records a verbatim event into the bounded replay ring.
func (s *claudeSession) appendEvent(raw json.RawMessage) {
	cp := make(json.RawMessage, len(raw))
	copy(cp, raw)
	s.ringMu.Lock()
	s.ring = append(s.ring, cp)
	if len(s.ring) > proto.ClaudeEventRingMax {
		s.ring = s.ring[len(s.ring)-proto.ClaudeEventRingMax:]
	}
	s.ringMu.Unlock()
}

// eventTail returns a copy of the recent-event ring for session_replay.
func (s *claudeSession) eventTail() []json.RawMessage {
	s.ringMu.Lock()
	defer s.ringMu.Unlock()
	out := make([]json.RawMessage, len(s.ring))
	copy(out, s.ring)
	return out
}

// claudeSessions is the process-global registry of live claude processes, keyed
// by sessionId. Mirrors terminals; NOT torn down on disconnect.
type claudeSessions struct {
	mu       sync.Mutex
	sessions map[string]*claudeSession
	sink     sink
	// baseCtx is the PROCESS-GLOBAL context (from Run). claude processes are
	// spawned from it so they survive a hub reconnect (D18); binding them to the
	// per-connection context would kill every session on a hub restart.
	baseCtx context.Context
}

func newClaudeSessions(baseCtx context.Context) *claudeSessions {
	return &claudeSessions{sessions: make(map[string]*claudeSession), baseCtx: baseCtx}
}

func (c *claudeSessions) put(s *claudeSession) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.sessions[s.id] = s
}

func (c *claudeSessions) get(id string) (*claudeSession, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	s, ok := c.sessions[id]
	return s, ok
}

func (c *claudeSessions) remove(id string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.sessions, id)
}

// descriptors snapshots live claude sessions for re-discovery (F).
func (c *claudeSessions) descriptors() []proto.SessionDescriptor {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]proto.SessionDescriptor, 0, len(c.sessions))
	for _, s := range c.sessions {
		out = append(out, proto.SessionDescriptor{
			SessionID:       s.id,
			Kind:            proto.SessionClaude,
			Cwd:             s.cwd,
			ClaudeSessionID: s.claudeID,
			PID:             s.pid,
			StartedAt:       s.startedAt.UTC().Format(time.RFC3339),
		})
	}
	return out
}

// close marks a claude session for explicit teardown. Returns true if it was
// live (so the caller emits a single session_exit).
func (c *claudeSessions) close(id string) bool {
	c.mu.Lock()
	s, ok := c.sessions[id]
	if ok {
		s.explicitClose.Store(true)
		delete(c.sessions, id)
	}
	c.mu.Unlock()
	if !ok {
		return false
	}
	s.release()
	return true
}

// claudeLauncher is the SINGLE isolated seam that builds the claude argv. Flags
// verified against claude 2.1.137. The hub assigns --session-id so the Lattice
// sessionId IS the claude session id; --resume reattaches a prior conversation
// from the Syncthing-synced transcript (D20).
func claudeLauncher(p proto.SessionCreatePayload) (string, []string) {
	name := resolveClaude()
	if name == "" {
		// Fall back to the bare name; exec will surface a clear not-found error
		// (placement should have excluded this agent, so this is defensive).
		name = "claude"
	}

	args := []string{
		"-p",
		"--verbose", // REQUIRED: claude rejects --print + --output-format=stream-json without it
		"--input-format", "stream-json",
		"--output-format", "stream-json",
		"--include-partial-messages",
		"--replay-user-messages",
		"--session-id", p.SessionID,
	}
	if p.SkipPerms {
		args = append(args, "--permission-mode", "bypassPermissions")
	} else {
		args = append(args, "--permission-mode", "default")
	}
	if p.ResumeID != "" {
		args = append(args, "--resume", p.ResumeID)
	}
	return name, args
}

// claudeChildEnv builds the environment for a spawned claude process, scrubbed of
// variables that would break subscription auth or confuse a nested launch. This
// matters because the agent itself may be started from INSIDE a Claude Code session
// (dogfooding, or the Tauri sidecar), inheriting an empty ANTHROPIC_API_KEY (which
// forces broken API-key auth → 401) plus CLAUDECODE/CLAUDE_CODE_* session markers.
// Removing ANTHROPIC_API_KEY forces the local Max subscription (OAuth) — which is
// both correct for the Claude tab (D17) and enforces the subscription-only cost rule.
func claudeChildEnv() []string {
	src := os.Environ()
	out := make([]string, 0, len(src))
	for _, kv := range src {
		key := kv
		if i := strings.IndexByte(kv, '='); i >= 0 {
			key = kv[:i]
		}
		switch {
		case key == "ANTHROPIC_API_KEY": // force subscription OAuth; never pay-per-token
			continue
		case key == "CLAUDECODE":
			continue
		case strings.HasPrefix(key, "CLAUDE_CODE_"): // nested-session markers (ENTRYPOINT, SESSION_ID, …)
			continue
		case key == "CLAUDE_AGENT_SDK_VERSION":
			continue
		}
		out = append(out, kv)
	}
	return out
}

// start spawns a claude process for the session, wires stdin/stdout/stderr, and
// launches the event pump + waiter. Returns pid (0 on failure) and any error.
func (c *claudeSessions) start(parent context.Context, p proto.SessionCreatePayload) (int, error) {
	if s, exists := c.get(p.SessionID); exists {
		return s.pid, nil // idempotent
	}

	_ = parent // process lifetime is rooted at baseCtx (process-global), see D18
	name, args := claudeLauncher(p)
	ctx, cancel := context.WithCancel(c.baseCtx)
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = p.Cwd
	cmd.Env = claudeChildEnv()

	stdin, err := cmd.StdinPipe()
	if err != nil {
		cancel()
		return 0, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		return 0, err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		cancel()
		return 0, err
	}

	if err := cmd.Start(); err != nil {
		cancel()
		return 0, err
	}

	sess := &claudeSession{
		id:        p.SessionID,
		cwd:       p.Cwd,
		claudeID:  p.SessionID,
		startedAt: time.Now(),
		cmd:       cmd,
		stdin:     stdin,
		cancel:    cancel,
	}
	if cmd.Process != nil {
		sess.pid = cmd.Process.Pid
	}
	c.put(sess)

	go c.pumpEvents(sess, stdout)
	go drainStderr(sess.id, stderr)

	go func() {
		_ = cmd.Wait()
		sess.release()
		c.remove(sess.id)
		if !sess.explicitClose.Load() {
			exitCode := 0
			if cmd.ProcessState != nil {
				exitCode = cmd.ProcessState.ExitCode()
			}
			c.sendExit(sess.id, exitCode, "")
		}
	}()

	return sess.pid, nil
}

// pumpEvents reads newline-delimited stream-json from claude stdout. Each line is
// forwarded verbatim as a claude_event (subtype = the line's top-level "type",
// lifted for cheap hub routing) and appended to the replay ring.
func (c *claudeSessions) pumpEvents(sess *claudeSession, stdout io.Reader) {
	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 0, 64<<10), claudeStdoutBuf)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		raw := make(json.RawMessage, len(line))
		copy(raw, line)

		var head struct {
			Type string `json:"type"`
		}
		_ = json.Unmarshal(raw, &head)

		sess.appendEvent(raw)

		frame, err := proto.Encode(proto.TypeClaudeEvent, proto.ClaudeEventPayload{
			SessionID: sess.id,
			Subtype:   head.Type,
			Raw:       raw,
		})
		if err != nil {
			log.Printf("agent: encode claude_event: %v", err)
			continue
		}
		c.sink.send(frame)
	}
	if err := scanner.Err(); err != nil {
		log.Printf("agent: claude %s stdout scan: %v", sess.id, err)
	}
}

// drainStderr logs claude stderr so launch/auth failures are diagnosable without
// leaking onto the event stream.
func drainStderr(sessionID string, stderr io.Reader) {
	scanner := bufio.NewScanner(stderr)
	scanner.Buffer(make([]byte, 0, 8<<10), 1<<20)
	for scanner.Scan() {
		log.Printf("agent: claude %s stderr: %s", sessionID, scanner.Text())
	}
}

// attach answers session_attach for a claude session: reply with the recent
// event tail so the (re)attaching browser rebuilds the conversation.
func (c *claudeSessions) attach(p proto.SessionAttachPayload) (proto.SessionReplayPayload, bool) {
	sess, ok := c.get(p.SessionID)
	if !ok {
		return proto.SessionReplayPayload{}, false
	}
	tail := sess.eventTail()
	return proto.SessionReplayPayload{
		SessionID: p.SessionID,
		Kind:      proto.SessionClaude,
		Events:    tail,
		Truncated: len(tail) >= proto.ClaudeEventRingMax,
	}, true
}

// input writes a user turn to claude stdin as a stream-json user message.
func (c *claudeSessions) input(p proto.ClaudeInputPayload) {
	sess, ok := c.get(p.SessionID)
	if !ok {
		return
	}
	msg := map[string]any{
		"type": "user",
		"message": map[string]any{
			"role": "user",
			"content": []map[string]any{
				{"type": "text", "text": p.Text},
			},
		},
	}
	c.writeStdin(sess, msg)
}

// permission answers a tool-permission prompt when approval mode is on. The exact
// control-line shape the claude CLI expects for stream-json approvals is NOT yet
// verified against 2.1.137 — TODO seam: confirm and adjust. We write a best-effort
// control message and never block on it.
func (c *claudeSessions) permission(p proto.ClaudePermissionPayload) {
	sess, ok := c.get(p.SessionID)
	if !ok {
		return
	}
	behavior := "deny"
	if p.Allow {
		behavior = "allow"
	}
	// TODO(verify): exact stream-json permission control shape on claude 2.1.137.
	msg := map[string]any{
		"type": "control_response",
		"response": map[string]any{
			"subtype":     "can_use_tool",
			"tool_use_id": p.ToolUseID,
			"behavior":    behavior,
		},
	}
	c.writeStdin(sess, msg)
}

// writeStdin marshals an object + newline to a claude session's stdin under its
// lock. Best-effort: stdin write failures are logged, not fatal.
func (c *claudeSessions) writeStdin(sess *claudeSession, obj any) {
	b, err := json.Marshal(obj)
	if err != nil {
		log.Printf("agent: claude %s marshal stdin: %v", sess.id, err)
		return
	}
	b = append(b, '\n')
	sess.stdinMu.Lock()
	defer sess.stdinMu.Unlock()
	if sess.stdin == nil {
		return
	}
	if _, err := sess.stdin.Write(b); err != nil {
		log.Printf("agent: claude %s stdin write: %v", sess.id, err)
	}
}

// sendExit emits a session_exit frame for a claude session.
func (c *claudeSessions) sendExit(sessionID string, code int, errMsg string) {
	frame, err := proto.Encode(proto.TypeSessionExit, proto.SessionControlPayload{
		SessionID: sessionID,
		ExitCode:  code,
		Error:     errMsg,
	})
	if err != nil {
		log.Printf("agent: encode session_exit: %v", err)
		return
	}
	c.sink.send(frame)
}
