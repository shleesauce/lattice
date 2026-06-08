package agent

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"log"
	"net"
	"net/url"
	"runtime"
	"strings"
	"time"

	"github.com/gorilla/websocket"
	"github.com/hashicorp/yamux"

	"github.com/shleesauce/lattice/internal/tunnel"
)

// editorDialTimeout bounds the agent's connect to its own loopback code-server
// when a tunnel stream arrives. Local, so short.
const editorDialTimeout = 5 * time.Second

// runTunnel maintains the SECOND outbound connection (D27): a persistent
// WebSocket to the hub's /ws/tunnel, with a yamux *client* session over it. The
// hub OPENS one stream per browser↔code-server connection; this loop ACCEPTS
// each, reads the sessionId handshake, and splices it to the right local
// code-server. It has its own reconnect/backoff (independent of the main
// /ws/agent link) and is rooted at the process ctx so it survives hub blips.
func runTunnel(ctx context.Context, agentWSURL string, cfg config, state *agentState) {
	tunnelURL, err := tunnelURLFrom(agentWSURL, cfg.token, computeAgentID(cfg.name))
	if err != nil {
		log.Printf("agent tunnel: bad url: %v", err)
		return
	}

	// Shares the agent's reconnect skeleton + jitter (M1/FIX 5). The tunnel has no
	// non-retryable rejection, so stop is always false.
	reconnectLoop(ctx, "agent tunnel", func() (stop, connected bool) {
		return false, serveTunnel(ctx, tunnelURL, state)
	})
}

// serveTunnel runs one tunnel connection lifecycle. Returns connected=true if the
// yamux session was established (so the caller resets backoff).
func serveTunnel(ctx context.Context, tunnelURL string, state *agentState) (connected bool) {
	conn, _, err := websocket.DefaultDialer.DialContext(ctx, tunnelURL, nil)
	if err != nil {
		log.Printf("agent tunnel: dial: %v", err)
		return false
	}

	sess, err := yamux.Client(tunnel.NewWSConn(conn), tunnel.Config())
	if err != nil {
		log.Printf("agent tunnel: yamux client: %v", err)
		conn.Close()
		return false
	}
	defer sess.Close()
	log.Printf("agent tunnel: established")

	for {
		stream, err := sess.AcceptStream()
		if err != nil {
			if ctx.Err() == nil {
				log.Printf("agent tunnel: accept ended: %v", err)
			}
			return true
		}
		go handleTunnelStream(stream, state)
	}
}

// handleTunnelStream splices one accepted tunnel stream to its loopback backend,
// chosen by the handshake: an editor session's code-server (resolved by
// sessionId) or an arbitrary dev-server port (preview). After the header the
// stream is a transparent pipe carrying the browser's HTTP/WebSocket bytes.
func handleTunnelStream(stream net.Conn, state *agentState) {
	defer stream.Close()

	br := bufio.NewReader(stream)
	target, err := tunnel.ReadStreamHeader(br)
	if err != nil {
		log.Printf("agent tunnel: read stream header: %v", err)
		return
	}

	var addr string
	switch target.Kind {
	case tunnel.KindPreview:
		// Preview dials any loopback dev server on this machine. Terminal/editor
		// access already imply this reach, so it's no new privilege — and binding
		// to 127.0.0.1 keeps nothing exposed on the agent's external interface (D2).
		addr = fmt.Sprintf("127.0.0.1:%d", target.Port)
	default:
		a, ok := state.editors.addrFor(target.SessionID)
		if !ok {
			log.Printf("agent tunnel: no editor for session %s", target.SessionID)
			return
		}
		addr = a
	}

	backend, err := net.DialTimeout("tcp", addr, editorDialTimeout)
	if err != nil {
		log.Printf("agent tunnel: dial backend %s: %v", addr, err)
		return
	}
	defer backend.Close()

	done := make(chan struct{}, 2)
	// br carries any bytes bufio buffered past the header plus the rest of the
	// stream, so the backend sees the full request.
	// Copy errors are the normal end-of-stream signal for a hijacked relay; the
	// deferred closes unblock the other direction, so they're intentionally ignored.
	go func() { _, _ = io.Copy(backend, br); done <- struct{}{} }()
	go func() { _, _ = io.Copy(stream, backend); done <- struct{}{} }()
	<-done
}

// tunnelURLFrom derives the /ws/tunnel URL (with token + agent query) from the
// already-resolved agent /ws/agent URL, so both connections target the same hub.
func tunnelURLFrom(agentWSURL, token, agentID string) (string, error) {
	u, err := url.Parse(agentWSURL)
	if err != nil {
		return "", err
	}
	u.Path = "/ws/tunnel"
	q := url.Values{}
	q.Set("token", token)
	q.Set("agent", agentID)
	u.RawQuery = q.Encode()
	return u.String(), nil
}

// computeAgentID mirrors the hub's agentID() derivation EXACTLY so the editor
// proxy finds this agent's tunnel by the session's agentId. Note the hub builds
// it from reg.OS (runtime.GOOS); we pass that in via osID().
func computeAgentID(hostname string) string {
	h := strings.ToLower(strings.TrimSpace(hostname))
	if h == "" {
		h = "unknown"
	}
	return h + "-" + osID()
}

// osID is runtime.GOOS lowercased/trimmed, matching the hub's agentID() os term.
func osID() string {
	return strings.ToLower(strings.TrimSpace(runtime.GOOS))
}
