package hub

import (
	"log"
	"net/http"
	"time"
)

// handleDashboardWS upgrades a browser connection, sends an immediate fleet
// snapshot, then keeps it subscribed to broadcasts until it disconnects.
func (h *Hub) handleDashboardWS(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("dashboard ws: upgrade failed: %v", err)
		return
	}

	d := &dashboardConn{conn: conn}
	h.registry.addDashboard(d)
	defer func() {
		h.registry.removeDashboard(d)
		conn.Close()
	}()

	// Initial snapshot on connect.
	if err := d.send(map[string]any{
		"type":   "fleet",
		"agents": h.fleet(),
	}); err != nil {
		return
	}

	// The dashboard is read-only from the hub's perspective; we drain inbound
	// frames only to detect close and keep the connection healthy. A read deadline
	// + ping/pong keepalive (mirroring the agent/terminal WS) bounds a half-open
	// socket so a silent browser can't leak this goroutine + conn forever. Every
	// pong (and any inbound frame) refreshes the deadline, so a healthy idle
	// dashboard is never disconnected.
	conn.SetReadLimit(1 << 16)
	conn.SetReadDeadline(time.Now().Add(dashboardReadTimeout))
	conn.SetPongHandler(func(string) error {
		return conn.SetReadDeadline(time.Now().Add(dashboardReadTimeout))
	})

	// Ping the browser on an interval well under the read timeout. Pings go through
	// d.ping (same writeMu the broadcasts use), so the keepalive never races a
	// concurrent push on the gorilla conn. A failed ping (dead socket) closes the
	// conn, which unblocks the ReadMessage loop below so the deferred cleanup runs.
	stop := make(chan struct{})
	defer close(stop)
	go func() {
		ticker := time.NewTicker(dashboardPingInterval)
		defer ticker.Stop()
		for {
			select {
			case <-stop:
				return
			case <-ticker.C:
				if err := d.ping(); err != nil {
					conn.Close()
					return
				}
			}
		}
	}()

	for {
		if _, _, err := conn.ReadMessage(); err != nil {
			return
		}
		// Any inbound frame also proves liveness — extend the window.
		conn.SetReadDeadline(time.Now().Add(dashboardReadTimeout))
	}
}
