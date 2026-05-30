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
	"net/url"
	"os"
	"runtime"
	"strings"
	"time"

	"github.com/gorilla/websocket"

	"github.com/dylanstoryyy/lattice/internal/proto"
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

	backoff := time.Second
	for {
		if ctx.Err() != nil {
			return nil
		}

		stop, connected, err := session(ctx, wsURL, cfg, version, state)
		if stop {
			// Bad token (or another non-retryable rejection): give up cleanly.
			return nil
		}
		if err != nil {
			log.Printf("agent: session ended: %v", err)
		}

		if ctx.Err() != nil {
			return nil
		}

		// A session that actually registered resets the backoff, so a long-lived
		// agent recovers in 1s after a hub blip instead of being stuck at the
		// ceiling for the rest of its life.
		if connected {
			backoff = time.Second
		}

		log.Printf("agent: reconnecting in %s", backoff)
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(backoff):
		}
		backoff *= 2
		if backoff > maxBackoff {
			backoff = maxBackoff
		}
	}
}

// parseFlags reads the agent flag set off args.
func parseFlags(args []string) (config, error) {
	fs := flag.NewFlagSet("agent", flag.ContinueOnError)
	var cfg config
	fs.StringVar(&cfg.hub, "hub", "", "hub address HOST:PORT (required), e.g. mini-ops.tail3c8bee.ts.net:7400")
	fs.StringVar(&cfg.token, "token", "", "enrollment token (required)")
	fs.StringVar(&cfg.name, "name", "", "agent display name (default: hostname)")
	if err := fs.Parse(args); err != nil {
		return config{}, err
	}

	if cfg.hub == "" {
		return config{}, errors.New("agent: --hub is required (HOST:PORT)")
	}
	if cfg.token == "" {
		return config{}, errors.New("agent: --token is required")
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
	descs := append(state.terms.descriptors(), state.claudes.descriptors()...)
	frame, err := proto.Encode(proto.TypeSessionListResult, proto.SessionListResultPayload{
		ReqID:    reqID,
		Sessions: descs,
	})
	if err != nil {
		log.Printf("agent: encode session_list_result: %v", err)
		return
	}
	select {
	case outbound <- frame:
	case <-ctx.Done():
	}
}

// writer is the sole goroutine permitted to write to conn. It drains outbound
// and sets a write deadline on every frame.
func writer(ctx context.Context, conn *websocket.Conn, outbound <-chan []byte) {
	for {
		select {
		case <-ctx.Done():
			// Best-effort close handshake.
			conn.SetWriteDeadline(time.Now().Add(time.Second))
			conn.WriteMessage(websocket.CloseMessage,
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
		hb := gatherMetrics(ctx)
		frame, err := proto.Encode(proto.TypeHeartbeat, hb)
		if err != nil {
			log.Printf("agent: encode heartbeat: %v", err)
			return
		}
		select {
		case outbound <- frame:
		case <-ctx.Done():
		}
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

		// --- Phase 3: claude channel ---
		case proto.TypeClaudeInput:
			var p proto.ClaudeInputPayload
			if err := proto.As(env, &p); err != nil {
				continue
			}
			state.claudes.input(p)
		case proto.TypeClaudePermission:
			var p proto.ClaudePermissionPayload
			if err := proto.As(env, &p); err != nil {
				continue
			}
			state.claudes.permission(p)

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

		case proto.TypeWake:
			var p proto.WakePayload
			if err := proto.As(env, &p); err != nil {
				log.Printf("agent: bad wake: %v", err)
				continue
			}
			go wake(ctx, p, outbound)

		default:
			log.Printf("agent: ignoring unexpected frame %q", env.Type)
		}
	}
}

// handleSessionCreate starts a terminal or claude session keyed by SessionID and
// acks with session_created (carrying pid + claudeSessionId, or an error).
func handleSessionCreate(ctx context.Context, outbound chan<- []byte, state *agentState, p proto.SessionCreatePayload) {
	ack := proto.SessionCreatedPayload{ReqID: p.ReqID, SessionID: p.SessionID}

	switch p.Kind {
	case proto.SessionClaude:
		pid, err := state.claudes.start(ctx, p)
		if err != nil {
			ack.Error = err.Error()
		} else {
			ack.PID = pid
			ack.ClaudeSessionID = p.SessionID // hub-assigned via --session-id
		}
	default: // terminal
		pid, err := state.terms.start(ctx, p)
		if err != nil {
			ack.Error = err.Error()
		} else {
			ack.PID = pid
		}
	}

	frame, err := proto.Encode(proto.TypeSessionCreated, ack)
	if err != nil {
		log.Printf("agent: encode session_created: %v", err)
		return
	}
	select {
	case outbound <- frame:
	case <-ctx.Done():
	}
}

// handleSessionAttach replies with a session_replay for the attached session.
func handleSessionAttach(ctx context.Context, outbound chan<- []byte, state *agentState, p proto.SessionAttachPayload) {
	var (
		replay proto.SessionReplayPayload
		ok     bool
	)
	if replay, ok = state.terms.attach(p); !ok {
		replay, ok = state.claudes.attach(p)
	}
	if !ok {
		return
	}
	frame, err := proto.Encode(proto.TypeSessionReplay, replay)
	if err != nil {
		log.Printf("agent: encode session_replay: %v", err)
		return
	}
	select {
	case outbound <- frame:
	case <-ctx.Done():
	}
}

// closeSession terminates a session of either kind and emits one session_exit.
func closeSession(state *agentState, sessionID string) {
	if state.terms.close(sessionID) {
		state.terms.sendExit(sessionID, 0, "")
		return
	}
	if state.claudes.close(sessionID) {
		state.claudes.sendExit(sessionID, 0, "")
	}
}
