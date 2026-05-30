package hub

import (
	"log"
	"net/http"

	"github.com/hashicorp/yamux"

	"github.com/dylanstoryyy/lattice/internal/tunnel"
)

// handleTunnelWS accepts an agent's SECOND dial-out WebSocket (D27) and runs a
// yamux session over it. The hub is the yamux *server* here only in the sense
// that it calls yamux.Server; it is the side that OPENS streams (one per browser
// ↔ code-server connection, see editorproxy.go) — the agent only accepts them.
//
//	GET /ws/tunnel?token=<enroll>&agent=<agentId>
//
// Auth: the same enrollment token gates this as gates /ws/agent. The agentId is
// the agent's own deterministic id (lowercase hostname + "-" + lowercase os),
// matching the hub's agentID() derivation, so the editor proxy can find this
// tunnel by the session's agentId.
func (h *Hub) handleTunnelWS(w http.ResponseWriter, r *http.Request) {
	if r.URL.Query().Get("token") != h.token {
		http.Error(w, "invalid token", http.StatusUnauthorized)
		return
	}
	agentID := r.URL.Query().Get("agent")
	if agentID == "" {
		http.Error(w, "agent is required", http.StatusBadRequest)
		return
	}

	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("tunnel ws: upgrade failed: %v", err)
		return
	}

	// yamux runs its own keepalive + framing over the WS-as-net.Conn. No read
	// deadline here: yamux's keepalive (tunnel.Config) detects a dead link and
	// closes the session, which unblocks the wait below and lets the agent's
	// reconnect loop re-establish.
	sess, err := yamux.Server(tunnel.NewWSConn(conn), tunnel.Config())
	if err != nil {
		log.Printf("tunnel ws: yamux server: %v", err)
		conn.Close()
		return
	}

	h.registry.putTunnel(agentID, sess)
	log.Printf("tunnel open: agent=%s", agentID)

	<-sess.CloseChan() // blocks until the session (and thus the WS) closes
	h.registry.removeTunnel(agentID, sess)
	_ = sess.Close()
	conn.Close()
	log.Printf("tunnel close: agent=%s", agentID)
}
