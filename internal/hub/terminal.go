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
// terminal/session WebSocket. It is a superset covering both terminal frames
// (input/resize) and claude frames (claude_input/claude_permission).
type browserTermMsg struct {
	Type string `json:"type"` // "input" | "resize" | "claude_input" | "claude_permission"
	Data string `json:"data"` // base64, for "input"
	Cols uint16 `json:"cols"` // for "resize"
	Rows uint16 `json:"rows"` // for "resize"
	// claude_input
	Text string `json:"text"`
	// claude_permission
	ToolUseID string `json:"toolUseId"`
	Allow     bool   `json:"allow"`
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

// handleSessionWS bridges a browser to a long-lived session (terminal OR claude)
// that already runs on an agent. Unlike /ws/terminal it does NOT create or close
// the process — it attaches, forwards live I/O, and on browser close DETACHES
// (the process keeps running). Create/close are REST (D18).
//
//	GET /ws/session?session=<id>&cols=<n>&rows=<n>
//
// Browser→hub framing: {"type":"input","data":b64} / {"type":"resize",cols,rows}
// for terminal; {"type":"claude_input","text":…} / {"type":"claude_permission",
// "toolUseId":…,"allow":bool} for claude. Hub→browser: {"type":"replay",…} then
// {"type":"output","data":b64} (terminal) or {"type":"claude_event",…} (claude),
// and {"type":"exit"}.
func (h *Hub) handleSessionWS(w http.ResponseWriter, r *http.Request) {
	sessionID := r.URL.Query().Get("session")
	if sessionID == "" {
		http.Error(w, "session is required", http.StatusBadRequest)
		return
	}

	rec, ok, err := h.store.GetSession(sessionID)
	if err != nil {
		http.Error(w, "session lookup failed", http.StatusInternalServerError)
		return
	}
	if !ok {
		http.Error(w, "unknown session", http.StatusNotFound)
		return
	}

	ac, online := h.registry.getAgent(rec.AgentID)
	if !online || !ac.isLive(offlineAfter) {
		http.Error(w, "session agent offline", http.StatusNotFound)
		return
	}

	cols := parseDim(r.URL.Query().Get("cols"), 80)
	rows := parseDim(r.URL.Query().Get("rows"), 24)

	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("session ws: upgrade failed: %v", err)
		return
	}

	// Reuse the terminal bridge machinery, keyed by sessionId. Only one browser
	// bridge per session at a time; a new attach replaces the old.
	t := &terminalConn{conn: conn, agentID: rec.AgentID}
	h.registry.putTerminal(sessionID, t)

	// Ask the agent to replay scrollback / event tail, then stream live frames.
	if err := ac.send(proto.TypeSessionAttach, proto.SessionAttachPayload{
		SessionID: sessionID, Cols: cols, Rows: rows,
	}); err != nil {
		_ = t.send(map[string]any{"type": "exit"})
		h.registry.removeTerminal(sessionID)
		t.close()
		return
	}

	log.Printf("session attach: session=%s agent=%s kind=%s", sessionID, rec.AgentID, rec.Kind)

	defer func() {
		// Browser closed → DETACH (keep the process running). Only drop the bridge.
		if cur, ok := h.registry.getAgent(rec.AgentID); ok {
			_ = cur.send(proto.TypeSessionDetach, proto.SessionControlPayload{SessionID: sessionID})
		}
		// Remove only if we are still the registered bridge (a newer attach may
		// have replaced us).
		if cur, ok := h.registry.getTerminal(sessionID); ok && cur == t {
			h.registry.removeTerminal(sessionID)
		}
		t.close()
		log.Printf("session detach: session=%s agent=%s", sessionID, rec.AgentID)
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
				TermID: sessionID, Data: msg.Data,
			}); err != nil {
				return
			}
		case "resize":
			if err := ac.send(proto.TypeTermResize, proto.TermResizePayload{
				TermID: sessionID, Cols: msg.Cols, Rows: msg.Rows,
			}); err != nil {
				return
			}
		case "claude_input":
			if err := ac.send(proto.TypeClaudeInput, proto.ClaudeInputPayload{
				SessionID: sessionID, Text: msg.Text,
			}); err != nil {
				return
			}
		case "claude_permission":
			if err := ac.send(proto.TypeClaudePermission, proto.ClaudePermissionPayload{
				SessionID: sessionID, ToolUseID: msg.ToolUseID, Allow: msg.Allow,
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
