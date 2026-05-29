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

	backoff := time.Second
	for {
		if ctx.Err() != nil {
			return nil
		}

		stop, connected, err := session(ctx, wsURL, cfg, version)
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
func session(ctx context.Context, wsURL string, cfg config, version string) (stop bool, connected bool, err error) {
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

	// Register first.
	regFrame, err := proto.Encode(proto.TypeRegister, proto.RegisterPayload{
		Token:        cfg.token,
		Hostname:     cfg.name,
		OS:           runtime.GOOS,
		Arch:         runtime.GOARCH,
		AgentVersion: version,
		Protocol:     proto.ProtocolVersion,
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

	// Heartbeat loop.
	go heartbeatLoop(sessCtx, outbound)

	// Per-session PTY registry; torn down with the connection.
	terms := newTerminals()
	defer terms.closeAll()

	// Read loop drives the session; it returns when the connection drops.
	readErr := readLoop(sessCtx, conn, outbound, terms)

	cancel()
	<-writerDone
	return false, true, readErr
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
func readLoop(ctx context.Context, conn *websocket.Conn, outbound chan<- []byte, terms *terminals) error {
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
			var p proto.TermStartPayload
			if err := proto.As(env, &p); err != nil {
				log.Printf("agent: bad term_start: %v", err)
				continue
			}
			terms.startTerm(ctx, p, outbound)
		case proto.TypeTermInput:
			var p proto.TermDataPayload
			if err := proto.As(env, &p); err != nil {
				continue
			}
			terms.input(p)
		case proto.TypeTermResize:
			var p proto.TermResizePayload
			if err := proto.As(env, &p); err != nil {
				continue
			}
			terms.resize(p)
		case proto.TypeTermClose:
			var p proto.TermControlPayload
			if err := proto.As(env, &p); err != nil {
				continue
			}
			if terms.close(p.TermID) {
				terms.sendExit(ctx, outbound, p.TermID, 0, "")
			}

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
