package agent

import (
	"bufio"
	"context"
	"io"
	"log"
	"net"
	"net/url"
	"runtime"
	"strings"
	"time"

	"github.com/gorilla/websocket"
	"github.com/hashicorp/yamux"

	"github.com/dylanstoryyy/lattice/internal/tunnel"
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

	backoff := time.Second
	for {
		if ctx.Err() != nil {
			return
		}
		connected := serveTunnel(ctx, tunnelURL, state)
		if ctx.Err() != nil {
			return
		}
		if connected {
			backoff = time.Second // a healthy session resets backoff
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff):
		}
		backoff *= 2
		if backoff > maxBackoff {
			backoff = maxBackoff
		}
	}
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
		go handleEditorStream(stream, state)
	}
}

// handleEditorStream splices one accepted tunnel stream to the loopback
// code-server named by its sessionId handshake. After the header the stream is a
// transparent pipe carrying the browser's HTTP/WebSocket bytes.
func handleEditorStream(stream net.Conn, state *agentState) {
	defer stream.Close()

	br := bufio.NewReader(stream)
	sessionID, err := tunnel.ReadStreamHeader(br)
	if err != nil {
		log.Printf("agent tunnel: read stream header: %v", err)
		return
	}
	addr, ok := state.editors.addrFor(sessionID)
	if !ok {
		log.Printf("agent tunnel: no editor for session %s", sessionID)
		return
	}

	backend, err := net.DialTimeout("tcp", addr, editorDialTimeout)
	if err != nil {
		log.Printf("agent tunnel: dial code-server %s: %v", addr, err)
		return
	}
	defer backend.Close()

	done := make(chan struct{}, 2)
	// br carries any bytes bufio buffered past the header plus the rest of the
	// stream, so the backend sees the full request.
	go func() { io.Copy(backend, br); done <- struct{}{} }()
	go func() { io.Copy(stream, backend); done <- struct{}{} }()
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
