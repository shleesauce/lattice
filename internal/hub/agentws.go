package hub

import (
	"log"
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
		conn:     conn,
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

// agentID derives a stable, deterministic id from hostname+os so reconnects
// update the same row.
func agentID(hostname, os string) string {
	h := strings.ToLower(strings.TrimSpace(hostname))
	if h == "" {
		h = "unknown"
	}
	return h + "-" + strings.ToLower(strings.TrimSpace(os))
}
