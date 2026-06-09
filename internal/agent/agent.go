// Package agent implements the Lattice leaf: it dials one persistent WebSocket
// out to the hub, registers, streams heartbeats with host metrics, and runs
// commands the hub dispatches — streaming their output back live.
//
// The hub never dials the agent; the single outbound connection is the only
// channel. gorilla/websocket connections are not safe for concurrent writers,
// so every send funnels through one writer goroutine via an outbound channel.
package agent

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"math/rand"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/gorilla/websocket"

	"github.com/shleesauce/lattice/internal/proto"
)

const (
	heartbeatInterval = 5 * time.Second
	writeTimeout      = 10 * time.Second
	maxBackoff        = 15 * time.Second
	outboundBuffer    = 64
)

// config holds the parsed CLI flags for the agent role.
type config struct {
	hub   string
	token string
	name  string
}

// Run is the agent entry point invoked by main for `lattice agent ...`.
func Run(ctx context.Context, args []string, version string) error {
	cfg, err := parseFlags(args)
	if err != nil {
		return err
	}

	wsURL, err := resolveURL(cfg.hub)
	if err != nil {
		return err
	}

	log.Printf("agent %s starting: name=%q hub=%s os=%s/%s", version, cfg.name, wsURL, runtime.GOOS, runtime.GOARCH)

	// Process-global session registry (D18): created once, survives every
	// reconnect so terminal + claude processes outlive the WebSocket. Rooted at
	// the process-lifetime ctx so sessions are NOT killed on a hub reconnect.
	state := newAgentState(ctx)

	// Second dial-out (D27): the editor tunnel. Its own persistent connection +
	// reconnect loop, independent of the main /ws/agent link, so code-server
	// traffic is multiplexed over yamux without a new inbound listener (D2).
	go runTunnel(ctx, wsURL, cfg, state)

	reconnectLoop(ctx, "agent", func() (stop, connected bool) {
		s, c, err := session(ctx, wsURL, cfg, version, state)
		if err != nil {
			log.Printf("agent: session ended: %v", err)
		}
		return s, c
	})
	return nil
}

// reconnectLoop drives a reconnecting outbound connection. It calls dial in a
// loop, sleeping an exponentially-backed-off interval between attempts. A dial
// that reports connected=true resets the backoff to 1s (so a long-lived link
// recovers fast after a hub blip instead of being stuck at the ceiling); a dial
// that reports stop=true is a non-retryable rejection (e.g. bad token) and ends
// the loop. Used by BOTH the main /ws/agent link and the editor tunnel, so the
// reconnect skeleton — and its jitter — lives in exactly one place (M1).
//
// Each sleep carries ±20% random jitter so that on a hub restart the whole fleet
// does NOT reconnect in synchronised waves (thundering herd, FIX 5). math/rand
// is fine here: this is load-spreading, not security-sensitive.
func reconnectLoop(ctx context.Context, label string, dial func() (stop, connected bool)) {
	backoff := time.Second
	for {
		if ctx.Err() != nil {
			return
		}

		stop, connected := dial()
		if stop {
			return
		}
		if ctx.Err() != nil {
			return
		}
		if connected {
			backoff = time.Second
		}

		sleep := jitter(backoff)
		log.Printf("%s: reconnecting in %s", label, sleep.Round(time.Millisecond))
		select {
		case <-ctx.Done():
			return
		case <-time.After(sleep):
		}
		backoff *= 2
		if backoff > maxBackoff {
			backoff = maxBackoff
		}
	}
}

// jitter applies ±20% random spread to d so synchronised reconnects fan out
// instead of stampeding the hub all at once (FIX 5).
func jitter(d time.Duration) time.Duration {
	delta := (rand.Float64()*2 - 1) * 0.2 // [-0.2, +0.2)
	return time.Duration(float64(d) * (1 + delta))
}

// parseFlags reads the agent flag set off args.
func parseFlags(args []string) (config, error) {
	fs := flag.NewFlagSet("agent", flag.ContinueOnError)
	var cfg config
	fs.StringVar(&cfg.hub, "hub", "", "hub address HOST:PORT (required), e.g. myhub.your-tailnet.ts.net:7400")
	fs.StringVar(&cfg.token, "token", "", "enrollment token (required)")
	fs.StringVar(&cfg.name, "name", "", "agent display name (default: hostname)")
	if err := fs.Parse(args); err != nil {
		return config{}, err
	}

	// Token precedence mirrors the hub: an explicit --token wins, otherwise fall
	// back to LATTICE_TOKEN. This lets deploy scripts/pm2/launchd hand the
	// credential to the process via the environment so it never lands in argv
	// (where it would be visible to anyone running `ps`).
	if cfg.token == "" {
		cfg.token = os.Getenv("LATTICE_TOKEN")
	}

	if cfg.hub == "" {
		return config{}, errors.New("agent: --hub is required (HOST:PORT)")
	}
	if cfg.token == "" {
		return config{}, errors.New("agent: --token is required (pass --token or set LATTICE_TOKEN)")
	}
	if cfg.name == "" {
		host, err := os.Hostname()
		if err != nil || host == "" {
			host = "lattice-agent"
		}
		cfg.name = host
	}
	return cfg, nil
}

// resolveURL turns the --hub value into a full ws:// URL ending in /ws/agent.
// If the value already carries a scheme it is respected; otherwise ws:// is used.
func resolveURL(hub string) (string, error) {
	raw := hub
	if !strings.Contains(raw, "://") {
		raw = "ws://" + raw
	}
	u, err := url.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("agent: invalid --hub %q: %w", hub, err)
	}
	switch u.Scheme {
	case "ws", "wss":
	case "http":
		u.Scheme = "ws"
	case "https":
		u.Scheme = "wss"
	default:
		return "", fmt.Errorf("agent: unsupported --hub scheme %q", u.Scheme)
	}
	if u.Host == "" {
		return "", fmt.Errorf("agent: --hub %q has no host", hub)
	}
	u.Path = "/ws/agent"
	u.RawQuery = ""
	return u.String(), nil
}

// session runs a single connection lifecycle. It returns stop=true only for a
// non-retryable rejection (e.g. bad token); otherwise the caller reconnects.
// The process-global state is shared across reconnects; this connection only
// swaps the output sink and drives the read loop.
func session(ctx context.Context, wsURL string, cfg config, version string, state *agentState) (stop bool, connected bool, err error) {
	conn, _, err := websocket.DefaultDialer.DialContext(ctx, wsURL, nil)
	if err != nil {
		return false, false, fmt.Errorf("dial %s: %w", wsURL, err)
	}
	defer conn.Close()

	// sessCtx is cancelled when this connection ends so all goroutines unwind.
	sessCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	outbound := make(chan []byte, outboundBuffer)

	// Single writer goroutine — the only place that writes to conn.
	writerDone := make(chan struct{})
	go func() {
		defer close(writerDone)
		writer(sessCtx, conn, outbound)
	}()

	// Register first. Capabilities ride the register frame so the hub can score
	// placement (D19) immediately, and are refreshed on every heartbeat.
	regFrame, err := proto.Encode(proto.TypeRegister, proto.RegisterPayload{
		Token:        cfg.token,
		Hostname:     cfg.name,
		OS:           runtime.GOOS,
		Arch:         runtime.GOARCH,
		AgentVersion: version,
		Protocol:     proto.ProtocolVersion,
		Capabilities: detectCapabilities(sessCtx),
	})
	if err != nil {
		return false, false, err
	}

	select {
	case outbound <- regFrame:
	case <-sessCtx.Done():
		return false, false, sessCtx.Err()
	}

	// Wait for the registered ack as the first frame.
	conn.SetReadDeadline(time.Now().Add(writeTimeout))
	_, raw, err := conn.ReadMessage()
	conn.SetReadDeadline(time.Time{})
	if err != nil {
		return false, false, fmt.Errorf("read registered ack: %w", err)
	}
	env, err := proto.Decode(raw)
	if err != nil {
		return false, false, err
	}
	if env.Type != proto.TypeRegistered {
		return false, false, fmt.Errorf("expected %q first, got %q", proto.TypeRegistered, env.Type)
	}
	var reg proto.RegisteredPayload
	if err := proto.As(env, &reg); err != nil {
		return false, false, err
	}
	if !reg.OK {
		log.Printf("agent: registration rejected: %s", reg.Error)
		return true, false, nil // non-retryable
	}
	log.Printf("agent: registered as %s", reg.AgentID)

	// Point every pump at THIS connection's outbound channel. On disconnect the
	// sink is cleared (nil) so live sessions buffer to their rings until a new
	// connection swaps the sink back in — processes never restart on reconnect.
	state.setSink(outbound)
	defer state.setSink(nil)

	// Heartbeat loop.
	go heartbeatLoop(sessCtx, outbound)

	// Re-discovery (F): volunteer the live sessions right after registered so the
	// hub re-adopts them. Sent as a follow-up frame (not inline in register) to
	// keep the register frame lean.
	sendSessionList(sessCtx, outbound, state, "")

	// Read loop drives the session; it returns when the connection drops.
	readErr := readLoop(sessCtx, conn, outbound, state)

	cancel()
	<-writerDone
	return false, true, readErr
}

// sendSessionList enumerates the live terminal + claude sessions and emits a
// session_list_result. reqID is empty for the post-register volunteer and set
// when answering a session_list request.
func sendSessionList(ctx context.Context, outbound chan<- []byte, state *agentState, reqID string) {
	descs := append(state.terms.descriptors(), state.editors.descriptors()...)
	sendFrame(ctx, outbound, proto.TypeSessionListResult, proto.SessionListResultPayload{
		ReqID:    reqID,
		Sessions: descs,
	})
}

// writer is the sole goroutine permitted to write to conn. It drains outbound
// and sets a write deadline on every frame.
func writer(ctx context.Context, conn *websocket.Conn, outbound <-chan []byte) {
	for {
		select {
		case <-ctx.Done():
			// Best-effort close handshake.
			conn.SetWriteDeadline(time.Now().Add(time.Second))
			_ = conn.WriteMessage(websocket.CloseMessage,
				websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
			return
		case b, ok := <-outbound:
			if !ok {
				return
			}
			conn.SetWriteDeadline(time.Now().Add(writeTimeout))
			if err := conn.WriteMessage(websocket.TextMessage, b); err != nil {
				log.Printf("agent: write error: %v", err)
				return
			}
		}
	}
}

// heartbeatLoop sends a heartbeat immediately, then every heartbeatInterval.
func heartbeatLoop(ctx context.Context, outbound chan<- []byte) {
	send := func() {
		sendFrame(ctx, outbound, proto.TypeHeartbeat, gatherMetrics(ctx))
	}

	send()
	ticker := time.NewTicker(heartbeatInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			send()
		}
	}
}

// readLoop consumes frames from the hub and dispatches them. It returns when
// the connection produces an error (drop, deadline, ctx cancel).
func readLoop(ctx context.Context, conn *websocket.Conn, outbound chan<- []byte, state *agentState) error {
	for {
		_, raw, err := conn.ReadMessage()
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return fmt.Errorf("read: %w", err)
		}

		env, err := proto.Decode(raw)
		if err != nil {
			log.Printf("agent: bad frame: %v", err)
			continue
		}

		switch env.Type {
		case proto.TypeRunCommand:
			var rc proto.RunCommandPayload
			if err := proto.As(env, &rc); err != nil {
				log.Printf("agent: bad run_command: %v", err)
				continue
			}
			go runCommand(ctx, rc, outbound)
		case proto.TypePing:
			// Keepalive: nothing to do. Control pings are handled by gorilla.

		case proto.TypeTermStart:
			// Back-compat: the legacy /ws/terminal endpoint opens an ephemeral PTY
			// via term_start. Map it onto the long-lived terminal machinery keyed by
			// TermID (treated as a sessionId). It survives reconnect like any session.
			var p proto.TermStartPayload
			if err := proto.As(env, &p); err != nil {
				log.Printf("agent: bad term_start: %v", err)
				continue
			}
			if _, err := state.terms.start(ctx, proto.SessionCreatePayload{
				SessionID: p.TermID, Kind: proto.SessionTerminal, Shell: p.Shell,
				Cols: p.Cols, Rows: p.Rows,
			}); err != nil {
				state.terms.sendLegacyExit(ctx, outbound, p.TermID, -1, err.Error())
			}
		case proto.TypeTermInput:
			var p proto.TermDataPayload
			if err := proto.As(env, &p); err != nil {
				continue
			}
			state.terms.input(p)
		case proto.TypeTermResize:
			var p proto.TermResizePayload
			if err := proto.As(env, &p); err != nil {
				continue
			}
			state.terms.resize(p)
		case proto.TypeTermClose:
			var p proto.TermControlPayload
			if err := proto.As(env, &p); err != nil {
				continue
			}
			if state.terms.close(p.TermID) {
				state.terms.sendLegacyExit(ctx, outbound, p.TermID, 0, "")
			}

		// --- Phase 3: long-lived sessions ---
		case proto.TypeSessionCreate:
			var p proto.SessionCreatePayload
			if err := proto.As(env, &p); err != nil {
				log.Printf("agent: bad session_create: %v", err)
				continue
			}
			go handleSessionCreate(ctx, outbound, state, p)
		case proto.TypeSessionAttach:
			var p proto.SessionAttachPayload
			if err := proto.As(env, &p); err != nil {
				continue
			}
			handleSessionAttach(ctx, outbound, state, p)
		case proto.TypeSessionDetach:
			// Keep the process running; the hub stops forwarding. Nothing to do.
		case proto.TypeSessionClose:
			var p proto.SessionControlPayload
			if err := proto.As(env, &p); err != nil {
				continue
			}
			closeSession(state, p.SessionID)
		case proto.TypeSessionList:
			var p proto.SessionListResultPayload // carries only an optional reqId
			_ = proto.As(env, &p)
			sendSessionList(ctx, outbound, state, p.ReqID)

		case proto.TypeFileList:
			var p proto.FileReqPayload
			if err := proto.As(env, &p); err != nil {
				log.Printf("agent: bad file_list: %v", err)
				continue
			}
			go listFiles(ctx, p, outbound)
		case proto.TypeFileGet:
			var p proto.FileReqPayload
			if err := proto.As(env, &p); err != nil {
				log.Printf("agent: bad file_get: %v", err)
				continue
			}
			go getFile(ctx, p, outbound)

		case proto.TypeTranscriptGet:
			var p proto.TranscriptReqPayload
			if err := proto.As(env, &p); err != nil {
				log.Printf("agent: bad transcript_get: %v", err)
				continue
			}
			go getTranscript(ctx, p, outbound)

		case proto.TypeWake:
			var p proto.WakePayload
			if err := proto.As(env, &p); err != nil {
				log.Printf("agent: bad wake: %v", err)
				continue
			}
			go wake(ctx, p, outbound)

		case proto.TypePowerControl:
			var p proto.PowerControlPayload
			if err := proto.As(env, &p); err != nil {
				log.Printf("agent: bad power_control: %v", err)
				continue
			}
			go powerControl(ctx, p, outbound)

		default:
			log.Printf("agent: ignoring unexpected frame %q", env.Type)
		}
	}
}

// handleSessionCreate starts a terminal or claude session keyed by SessionID and
// acks with session_created (carrying pid + claudeSessionId, or an error).
func handleSessionCreate(ctx context.Context, outbound chan<- []byte, state *agentState, p proto.SessionCreatePayload) {
	ack := proto.SessionCreatedPayload{ReqID: p.ReqID, SessionID: p.SessionID}

	// Resolve the working directory locally. A device session sends an empty (or
	// "~") cwd meaning "this machine's home" — the hub can't know the home path
	// because it differs per box (/Users/alice vs /Users/bob vs C:\Users\…).
	p.Cwd = resolveCwd(p.Cwd)

	switch p.Kind {
	case proto.SessionEditor:
		pid, err := state.editors.start(ctx, p)
		if err != nil {
			ack.Error = err.Error()
		} else {
			ack.PID = pid
		}
	default: // terminal OR claude — both are interactive PTYs (D35)
		pid, err := state.terms.start(ctx, p)
		if err != nil {
			ack.Error = err.Error()
		} else {
			ack.PID = pid
			if p.Kind == proto.SessionClaude {
				ack.ClaudeSessionID = p.SessionID // hub-assigned via --session-id
			}
		}
	}

	sendFrame(ctx, outbound, proto.TypeSessionCreated, ack)
}

// resolveCwd maps a session's working directory to a path valid on THIS machine.
//
//   - Empty / "~" / "~/sub" → the agent's home dir (device sessions).
//   - A path under SOME machine's home (e.g. a folder-synced project) → re-rooted
//     at this machine's $HOME (D23). A project folder is synced fleet-wide, but
//     each machine's home differs (/Users/alice vs /home/bob vs C:\Users\…), so
//     the hub's absolute path is wrong on a remote agent. We detect a leading home
//     prefix in the incoming path and rebase the remainder onto the local $HOME,
//     so a project session placed on ANY machine opens the right local copy — for
//     the editor, claude, and terminal alike. No project name is hardcoded.
//   - Any other absolute path → untouched.
//
// On home-lookup failure it returns the input unchanged so the OS default applies.
func resolveCwd(cwd string) string {
	if rest, ok := stripHomePrefix(cwd); ok {
		if home, err := os.UserHomeDir(); err == nil && home != "" {
			// filepath.Join normalises separators for the local OS (e.g. backslashes
			// inside WSL/Windows).
			return filepath.Join(home, rest)
		}
	}
	if cwd != "" && cwd != "~" && !strings.HasPrefix(cwd, "~/") {
		return cwd
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return cwd
	}
	if cwd == "" || cwd == "~" {
		return home
	}
	return filepath.Join(home, cwd[2:]) // "~/sub/dir"
}

// stripHomePrefix detects a leading per-user home prefix in an absolute path and
// returns the path remainder relative to that home (slash-normalised), so the
// caller can rebase it onto the LOCAL machine's $HOME. It recognises the common
// shapes that a synced folder's absolute path takes across machines:
//
//	/Users/<name>/<rest>   (macOS)
//	/home/<name>/<rest>    (Linux)
//	/root/<rest>           (Linux root)
//	C:\Users\<name>\<rest> (Windows; any drive letter, '/' or '\' separators)
//
// It returns ("", false) when no home prefix is present (the path is left alone).
func stripHomePrefix(path string) (string, bool) {
	// Windows: <drive>:\Users\<name>\<rest> (also tolerate forward slashes).
	if len(path) >= 3 && path[1] == ':' && (path[2] == '\\' || path[2] == '/') {
		norm := strings.ReplaceAll(path, "\\", "/")
		if rest, ok := afterSegments(norm[2:], "Users"); ok {
			return rest, true
		}
		return "", false
	}
	if rest, ok := afterSegments(path, "Users"); ok { // /Users/<name>/<rest>
		return rest, true
	}
	if rest, ok := afterSegments(path, "home"); ok { // /home/<name>/<rest>
		return rest, true
	}
	if strings.HasPrefix(path, "/root/") { // /root/<rest>
		return path[len("/root/"):], true
	}
	return "", false
}

// afterSegments matches "/<dir>/<name>/<rest>" on a slash-separated path and
// returns "<rest>". It requires a non-empty <name> segment and a non-empty
// remainder so a bare "/Users/alice" (no project under it) isn't rebased.
func afterSegments(path, dir string) (string, bool) {
	prefix := "/" + dir + "/"
	if !strings.HasPrefix(path, prefix) {
		return "", false
	}
	tail := path[len(prefix):] // "<name>/<rest>"
	slash := strings.IndexByte(tail, '/')
	if slash <= 0 || slash == len(tail)-1 {
		return "", false
	}
	return tail[slash+1:], true
}

// handleSessionAttach replies with a session_replay for the attached session.
func handleSessionAttach(ctx context.Context, outbound chan<- []byte, state *agentState, p proto.SessionAttachPayload) {
	var (
		replay proto.SessionReplayPayload
		ok     bool
	)
	if replay, ok = state.terms.attach(p); !ok {
		replay, ok = state.editors.attach(p) // editors have no replay (ok=false)
	}
	if !ok {
		return
	}
	sendFrame(ctx, outbound, proto.TypeSessionReplay, replay)
}

// closeSession terminates a session of either kind and emits one session_exit.
func closeSession(state *agentState, sessionID string) {
	if state.terms.close(sessionID) {
		state.terms.sendExit(sessionID, 0, "")
		return
	}
	if state.editors.close(sessionID) {
		state.editors.sendExit(sessionID, 0, "")
	}
}
