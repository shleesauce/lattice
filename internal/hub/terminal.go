package hub

import (
	"encoding/json"
	"log"
	"net/http"
	"strconv"
	"time"

	"github.com/dylanstoryyy/lattice/internal/proto"
)

// browserTermMsg is the JSON framing the browser sends to the hub over the
// terminal WebSocket.
type browserTermMsg struct {
	Type string `json:"type"` // "input" | "resize"
	Data string `json:"data"` // base64, for "input"
	Cols uint16 `json:"cols"` // for "resize"
	Rows uint16 `json:"rows"` // for "resize"
}

// handleTerminalWS bridges a browser interactive terminal to an agent PTY.
//
//	GET /ws/terminal?agent=<agentId>&cols=<n>&rows=<n>
//
// Framing browser→hub: {"type":"input","data":"<base64>"} and
// {"type":"resize","cols":N,"rows":N}. Framing hub→browser:
// {"type":"output","data":"<base64>"} and {"type":"exit"}.
//
// The hub allocates a termId, opens a PTY on the agent over the agent's main
// WS, and relays bytes until either side closes.
func (h *Hub) handleTerminalWS(w http.ResponseWriter, r *http.Request) {
	agentID := r.URL.Query().Get("agent")
	if agentID == "" {
		http.Error(w, "agent is required", http.StatusBadRequest)
		return
	}

	ac, ok := h.registry.getAgent(agentID)
	if !ok || !ac.isLive(offlineAfter) {
		http.Error(w, "agent offline", http.StatusNotFound)
		return
	}

	cols := parseDim(r.URL.Query().Get("cols"), 80)
	rows := parseDim(r.URL.Query().Get("rows"), 24)

	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("terminal ws: upgrade failed: %v", err)
		return
	}

	termID := newTermID()
	t := &terminalConn{conn: conn, agentID: agentID}
	h.registry.putTerminal(termID, t)

	// Ask the agent to open the PTY. On failure tear down immediately.
	if err := ac.send(proto.TypeTermStart, proto.TermStartPayload{
		TermID: termID, Cols: cols, Rows: rows,
	}); err != nil {
		_ = t.send(map[string]any{"type": "exit"})
		h.registry.removeTerminal(termID)
		t.close()
		return
	}

	log.Printf("terminal open: agent=%s term=%s %dx%d", agentID, termID, cols, rows)

	defer func() {
		// Best-effort tell the agent to close the PTY, then tear down the bridge.
		if cur, ok := h.registry.getAgent(agentID); ok {
			_ = cur.send(proto.TypeTermClose, proto.TermControlPayload{TermID: termID})
		}
		h.registry.removeTerminal(termID)
		t.close()
		log.Printf("terminal close: agent=%s term=%s", agentID, termID)
	}()

	conn.SetReadLimit(1 << 20)
	for {
		conn.SetReadDeadline(time.Now().Add(terminalReadTimeout))
		_, raw, err := conn.ReadMessage()
		if err != nil {
			return
		}

		var msg browserTermMsg
		if err := json.Unmarshal(raw, &msg); err != nil {
			continue
		}

		switch msg.Type {
		case "input":
			if err := ac.send(proto.TypeTermInput, proto.TermDataPayload{
				TermID: termID, Data: msg.Data,
			}); err != nil {
				return
			}
		case "resize":
			if err := ac.send(proto.TypeTermResize, proto.TermResizePayload{
				TermID: termID, Cols: msg.Cols, Rows: msg.Rows,
			}); err != nil {
				return
			}
		default:
			// Ignore unknown browser frame types.
		}
	}
}

// parseDim parses a positive uint16 terminal dimension, falling back to def.
func parseDim(s string, def uint16) uint16 {
	if s == "" {
		return def
	}
	n, err := strconv.ParseUint(s, 10, 16)
	if err != nil || n == 0 {
		return def
	}
	return uint16(n)
}
