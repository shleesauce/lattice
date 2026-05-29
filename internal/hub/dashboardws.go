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
	// frames only to detect close and keep the connection healthy.
	conn.SetReadLimit(1 << 16)
	conn.SetReadDeadline(time.Time{})
	for {
		if _, _, err := conn.ReadMessage(); err != nil {
			return
		}
	}
}
