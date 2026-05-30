package hub

import (
	"log"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/gorilla/websocket"

	"github.com/dylanstoryyy/lattice/internal/proto"
)

// handleAgentWS upgrades an inbound agent connection, requires a valid register
// frame, then services heartbeats and command results until disconnect.
func (h *Hub) handleAgentWS(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("agent ws: upgrade failed: %v", err)
		return
	}

	ac, err := h.register(conn)
	if err != nil {
		log.Printf("agent ws: registration rejected: %v", err)
		conn.Close()
		return
	}

	h.registry.putAgent(ac)
	h.broadcastFleet()
	log.Printf("agent register: id=%s host=%s os=%s/%s v=%s", ac.id, ac.hostname, ac.os, ac.arch, ac.version)

	defer func() {
		h.registry.removeAgent(ac)
		conn.Close()
		// Sessions on this agent are now unreachable: keep the rows but mark them
		// orphaned so the UI shows them as resumable elsewhere (D20).
		if err := h.store.MarkAgentSessionsOrphaned(ac.id); err != nil {
			log.Printf("agent %s: orphan sessions failed: %v", ac.id, err)
		}
		h.broadcastFleet()
		log.Printf("agent disconnect: id=%s", ac.id)
	}()

	h.readLoop(ac)
}

// register reads and validates the mandatory first register frame.
func (h *Hub) register(conn *websocket.Conn) (*agentConn, error) {
	conn.SetReadDeadline(time.Now().Add(10 * time.Second))
	_, raw, err := conn.ReadMessage()
	if err != nil {
		return nil, err
	}
	conn.SetReadDeadline(time.Time{})

	env, err := proto.Decode(raw)
	if err != nil {
		return nil, err
	}
	if env.Type != proto.TypeRegister {
		ackReject(conn, "first frame must be register")
		return nil, errFirstFrame
	}

	var reg proto.RegisterPayload
	if err := proto.As(env, &reg); err != nil {
		ackReject(conn, "bad register payload")
		return nil, err
	}
	if reg.Token != h.token {
		ackReject(conn, "invalid token")
		return nil, errBadToken
	}

	id := agentID(reg.Hostname, reg.OS)
	name := reg.Hostname
	now := time.Now()

	ac := &agentConn{
		id:       id,
		name:     name,
		hostname: reg.Hostname,
		os:       reg.OS,
		arch:     reg.Arch,
		version:  reg.AgentVersion,
		caps:     reg.Capabilities,
		conn:     conn,
		local:    isLoopbackAddr(conn.RemoteAddr()),
		lastSeen: now,
		online:   true,
	}

	if err := h.store.UpsertAgent(AgentRecord{
		ID: id, Name: name, Hostname: reg.Hostname, OS: reg.OS, Arch: reg.Arch,
		AgentVersion: reg.AgentVersion, FirstSeen: now, LastSeen: now,
	}); err != nil {
		log.Printf("agent register: upsert failed: %v", err)
	}

	if err := ac.send(proto.TypeRegistered, proto.RegisteredPayload{AgentID: id, OK: true}); err != nil {
		return nil, err
	}
	return ac, nil
}

// readLoop processes inbound frames from an established agent connection.
func (h *Hub) readLoop(ac *agentConn) {
	for {
		// Refresh the liveness window on every iteration: healthy agents keep it
		// alive via 5s heartbeats; a half-open socket trips the deadline and the
		// read errors out, letting the deferred cleanup run.
		ac.conn.SetReadDeadline(time.Now().Add(agentReadTimeout))
		_, raw, err := ac.conn.ReadMessage()
		if err != nil {
			return
		}
		env, err := proto.Decode(raw)
		if err != nil {
			log.Printf("agent %s: decode error: %v", ac.id, err)
			continue
		}

		switch env.Type {
		case proto.TypeHeartbeat:
			var hb proto.HeartbeatPayload
			if err := proto.As(env, &hb); err != nil {
				continue
			}
			now := time.Now()
			ac.updateHeartbeat(hb, now)
			if err := h.store.UpdateMetrics(ac.id, hb, now); err != nil {
				log.Printf("agent %s: metrics persist failed: %v", ac.id, err)
			}
			h.broadcastFleet()

		case proto.TypeCommandOutput:
			var out proto.CommandOutputPayload
			if err := proto.As(env, &out); err != nil {
				continue
			}
			h.registry.broadcast(map[string]any{
				"type":    "output",
				"agentId": ac.id,
				"cmdId":   out.CmdID,
				"stream":  out.Stream,
				"data":    out.Data,
			})

		case proto.TypeCommandExit:
			var ex proto.CommandExitPayload
			if err := proto.As(env, &ex); err != nil {
				continue
			}
			if err := h.store.FinishCommand(ex.CmdID, ex.ExitCode, ex.Error, time.Now()); err != nil {
				log.Printf("agent %s: command finish persist failed: %v", ac.id, err)
			}
			h.registry.broadcast(map[string]any{
				"type":     "exit",
				"agentId":  ac.id,
				"cmdId":    ex.CmdID,
				"exitCode": ex.ExitCode,
				"error":    ex.Error,
			})

		case proto.TypeTermOutput:
			var d proto.TermDataPayload
			if err := proto.As(env, &d); err != nil {
				continue
			}
			if t, ok := h.registry.getTerminal(d.TermID); ok {
				if err := t.send(map[string]any{"type": "output", "data": d.Data}); err != nil {
					t.close()
					h.registry.removeTerminal(d.TermID)
				}
			}

		case proto.TypeTermExit:
			var c proto.TermControlPayload
			if err := proto.As(env, &c); err != nil {
				continue
			}
			if t, ok := h.registry.getTerminal(c.TermID); ok {
				_ = t.send(map[string]any{"type": "exit"})
				t.close()
				h.registry.removeTerminal(c.TermID)
			}

		case proto.TypeFileListResult:
			var res proto.FileListResultPayload
			if err := proto.As(env, &res); err != nil {
				continue
			}
			h.registry.resolvePending(res.ReqID, env)

		case proto.TypeFileGetResult:
			var res proto.FileGetResultPayload
			if err := proto.As(env, &res); err != nil {
				continue
			}
			h.registry.resolvePending(res.ReqID, env)

		case proto.TypeWakeResult:
			var res proto.WakeResultPayload
			if err := proto.As(env, &res); err != nil {
				continue
			}
			h.registry.resolvePending(res.ReqID, env)

		// --- Phase 3: session lifecycle + claude channel ---
		case proto.TypeSessionCreated:
			var res proto.SessionCreatedPayload
			if err := proto.As(env, &res); err != nil {
				continue
			}
			h.registry.resolvePending(res.ReqID, env)

		case proto.TypeSessionListResult:
			var res proto.SessionListResultPayload
			if err := proto.As(env, &res); err != nil {
				continue
			}
			// Volunteered post-register list is re-discovery; a list with a reqId is
			// an answer to a round-trip. Both reconcile the DB; the reqId case also
			// resolves the waiter.
			h.adoptSessions(ac.id, res.Sessions)
			if res.ReqID != "" {
				h.registry.resolvePending(res.ReqID, env)
			}

		case proto.TypeSessionReplay:
			var p proto.SessionReplayPayload
			if err := proto.As(env, &p); err != nil {
				continue
			}
			if t, ok := h.registry.getTerminal(p.SessionID); ok {
				msg := map[string]any{"type": "replay", "kind": string(p.Kind), "truncated": p.Truncated}
				if p.Kind == proto.SessionClaude {
					msg["events"] = p.Events
				} else {
					msg["data"] = p.Data
				}
				if err := t.send(msg); err != nil {
					t.close()
					h.registry.removeTerminal(p.SessionID)
				}
			}

		case proto.TypeClaudeEvent:
			var p proto.ClaudeEventPayload
			if err := proto.As(env, &p); err != nil {
				continue
			}
			h.auditClaudeEvent(ac.id, p)
			if t, ok := h.registry.getTerminal(p.SessionID); ok {
				if err := t.send(map[string]any{
					"type": "claude_event", "subtype": p.Subtype, "raw": p.Raw,
				}); err != nil {
					t.close()
					h.registry.removeTerminal(p.SessionID)
				}
			}

		case proto.TypeSessionExit:
			var p proto.SessionControlPayload
			if err := proto.As(env, &p); err != nil {
				continue
			}
			if err := h.store.UpdateSessionStatus(p.SessionID, proto.SessionExited, time.Now()); err != nil {
				log.Printf("agent %s: session exit persist failed: %v", ac.id, err)
			}
			if t, ok := h.registry.getTerminal(p.SessionID); ok {
				_ = t.send(map[string]any{"type": "exit"})
				t.close()
				h.registry.removeTerminal(p.SessionID)
			}

		default:
			// Ignore unknown / hub-bound types received from an agent.
		}
	}
}

// ackReject best-effort sends a rejection ack before the caller closes.
func ackReject(conn *websocket.Conn, msg string) {
	b, err := proto.Encode(proto.TypeRegistered, proto.RegisteredPayload{OK: false, Error: msg})
	if err != nil {
		return
	}
	conn.WriteMessage(websocket.TextMessage, b)
}

// isLoopbackAddr reports whether a connection's remote address is loopback
// (127.0.0.1 / ::1), i.e. the agent runs on the hub host itself. Used to prefer
// the co-located agent for project scaffolding (POST /api/projects).
func isLoopbackAddr(addr net.Addr) bool {
	if addr == nil {
		return false
	}
	host := addr.String()
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	ip := net.ParseIP(strings.TrimSpace(host))
	return ip != nil && ip.IsLoopback()
}

// agentID derives a stable, deterministic id from hostname+os so reconnects
// update the same row.
func agentID(hostname, os string) string {
	h := strings.ToLower(strings.TrimSpace(hostname))
	if h == "" {
		h = "unknown"
	}
	return h + "-" + strings.ToLower(strings.TrimSpace(os))
}
