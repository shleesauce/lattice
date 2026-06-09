package hub

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/shleesauce/lattice/internal/proto"
)

// errRoundTripTimeout is returned when an agent does not answer a correlated
// request within pendingTimeout.
var errRoundTripTimeout = errors.New("agent did not respond in time")

// errFromAgent wraps an error string an agent returned in a payload (e.g. a
// failed session_create) so callers get a plain error value.
func errFromAgent(msg string) error { return errors.New(msg) }

// roundTrip sends a request frame to an online agent and waits (bounded by
// pendingTimeout) for the matching result, routed back by the agentws read loop
// via the pending-request map keyed by reqId. The registry lock is never held
// across the wait, so this cannot deadlock the read loop.
func (h *Hub) roundTrip(agentID, reqID string, t proto.MessageType, payload any) (proto.Envelope, error) {
	return h.roundTripT(agentID, reqID, pendingTimeout, t, payload)
}

// roundTripT is roundTrip with an explicit deadline, for requests whose agent-side
// work legitimately runs longer than the default pendingTimeout (e.g. a fleet
// update, where the agent downloads + verifies + swaps a binary before replying).
func (h *Hub) roundTripT(agentID, reqID string, timeout time.Duration, t proto.MessageType, payload any) (proto.Envelope, error) {
	ac, ok := h.registry.liveAgent(agentID)
	if !ok {
		return proto.Envelope{}, errors.New("agent offline")
	}

	ch := h.registry.registerPending(reqID)
	if err := ac.send(t, payload); err != nil {
		h.registry.clearPending(reqID)
		return proto.Envelope{}, err
	}

	select {
	case env := <-ch:
		return env, nil
	case <-time.After(timeout):
		h.registry.clearPending(reqID)
		return proto.Envelope{}, errRoundTripTimeout
	}
}

// handleFiles answers GET /api/agents/{id}/files?path=<p> with the agent's
// directory listing (FileListResultPayload JSON).
func (h *Hub) handleFiles(w http.ResponseWriter, r *http.Request, agentID string) {
	reqID := newReqID()
	env, err := h.roundTrip(agentID, reqID, proto.TypeFileList, proto.FileReqPayload{
		ReqID: reqID, Path: r.URL.Query().Get("path"),
	})
	if err != nil {
		writeJSON(w, statusForRoundTrip(err), map[string]any{"error": err.Error()})
		return
	}

	var res proto.FileListResultPayload
	if err := proto.As(env, &res); err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]any{"error": "bad agent response"})
		return
	}
	// A path-level error (e.g. permission denied) is carried inside the payload,
	// so this is a 200 either way — the listing response and the error response
	// share the same write.
	writeJSON(w, http.StatusOK, res)
}

// handleDownload answers GET /api/agents/{id}/download?path=<p> by streaming
// the raw file bytes (the hub base64-decodes the agent's FileGetResult.Content)
// with a Content-Disposition: attachment header.
func (h *Hub) handleDownload(w http.ResponseWriter, r *http.Request, agentID string) {
	filePath := r.URL.Query().Get("path")
	if strings.TrimSpace(filePath) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "path is required"})
		return
	}

	reqID := newReqID()
	env, err := h.roundTrip(agentID, reqID, proto.TypeFileGet, proto.FileReqPayload{
		ReqID: reqID, Path: filePath,
	})
	if err != nil {
		writeJSON(w, statusForRoundTrip(err), map[string]any{"error": err.Error()})
		return
	}

	var res proto.FileGetResultPayload
	if err := proto.As(env, &res); err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]any{"error": "bad agent response"})
		return
	}
	if res.Error != "" {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": res.Error})
		return
	}

	raw, err := base64.StdEncoding.DecodeString(res.Content)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]any{"error": "corrupt file content"})
		return
	}

	name := res.Name
	if name == "" {
		name = path.Base(filePath)
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", "attachment; filename=\""+sanitizeFilename(name)+"\"")
	w.Header().Set("Content-Length", strconv.Itoa(len(raw)))
	if res.Truncated {
		w.Header().Set("X-Lattice-Truncated", "true")
	}
	w.WriteHeader(http.StatusOK)
	if _, err := w.Write(raw); err != nil {
		log.Printf("download: write failed agent=%s path=%q: %v", agentID, filePath, err)
	}
}

// handleWake answers POST /api/agents/{senderId}/wake with body {"mac":"..."}.
// The sender is an online agent on the target's LAN that broadcasts the WoL
// magic packet.
//
// Body forms (both accepted, back-compat):
//
//	{"mac":"aa:bb:.."}         — explicit MAC, {senderId} is the relay (legacy).
//	{}  (and {senderId}==target) — RELAY-AWARE: treat {senderId} as the TARGET
//	    machine to wake, let the hub pick a live relay on the target's subnet and
//	    use the target's last-known MAC. Surfaces "no relay reachable on that
//	    subnet" instead of failing silently.
func (h *Hub) handleWake(w http.ResponseWriter, r *http.Request, senderID string) {
	var body struct {
		MAC string `json:"mac"`
	}
	// An empty body is valid (relay-aware form); decode best-effort.
	if r.Body != nil {
		_ = json.NewDecoder(r.Body).Decode(&body)
	}

	// Relay-aware form: no explicit MAC ⇒ {senderId} is the TARGET. The hub
	// resolves the relay + MAC from the target's last-known LAN/MACs.
	if strings.TrimSpace(body.MAC) == "" {
		h.wakeTarget(w, senderID)
		return
	}

	// Legacy form: explicit MAC, {senderId} is the relay.
	h.relayWake(w, senderID, body.MAC, false, "")
}

// wakeTarget resolves a relay on the target's subnet and emits the magic packet.
// targetID is the OFFLINE machine to wake.
func (h *Hub) wakeTarget(w http.ResponseWriter, targetID string) {
	fleet := h.fleet()
	var target *Agent
	for i := range fleet {
		if fleet[i].ID == targetID {
			target = &fleet[i]
			break
		}
	}
	if target == nil {
		writeJSON(w, http.StatusNotFound, map[string]any{"ok": false, "error": "unknown machine"})
		return
	}
	if len(target.MACs) == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "no known MAC for this machine — can't wake it"})
		return
	}

	choice := selectWakeRelay(wakeTargetForAgent(*target), fleet)
	if choice.RelayID == "" {
		log.Printf("wake: target=%s no relay (%s) tried=%v", targetID, choice.Reason, choice.Tried)
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{
			"ok": false, "error": choice.Reason, "relay": "", "onSubnet": false,
		})
		return
	}
	h.relayWake(w, choice.RelayID, choice.MAC, choice.OnSubnet, choice.Subnet)
}

// relayWake sends the magic packet for mac via the given relay agent and writes
// the JSON result (echoing which relay/subnet was used so the UI can show it).
func (h *Hub) relayWake(w http.ResponseWriter, relayID, mac string, onSubnet bool, subnet string) {
	reqID := newReqID()
	env, err := h.roundTrip(relayID, reqID, proto.TypeWake, proto.WakePayload{
		ReqID: reqID, MAC: mac,
	})
	if err != nil {
		writeJSON(w, statusForRoundTrip(err), map[string]any{"ok": false, "error": err.Error(), "relay": relayID})
		return
	}

	var res proto.WakeResultPayload
	if err := proto.As(env, &res); err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]any{"ok": false, "error": "bad agent response", "relay": relayID})
		return
	}
	log.Printf("wake: relay=%s mac=%s onSubnet=%v subnet=%s ok=%v err=%q", relayID, mac, onSubnet, subnet, res.OK, res.Error)
	writeJSON(w, http.StatusOK, map[string]any{
		"ok": res.OK, "error": res.Error, "relay": relayID, "onSubnet": onSubnet, "subnet": subnet,
	})
}

// handlePower answers POST /api/agents/{id}/power with body
// {"action":"sleep"|"shutdown"}, asking the target agent to suspend or power off
// its OWN machine — the close of the unattended loop (wake → work → sleep). The
// agent acks before it goes offline, so a successful sleep returns ok=true and
// then the agent drops from the fleet.
func (h *Hub) handlePower(w http.ResponseWriter, r *http.Request, agentID string) {
	var body struct {
		Action string `json:"action"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "invalid json body"})
		return
	}
	action := proto.PowerAction(strings.TrimSpace(body.Action))
	if action != proto.PowerSleep && action != proto.PowerShutdown {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "action must be sleep or shutdown"})
		return
	}

	reqID := newReqID()
	env, err := h.roundTrip(agentID, reqID, proto.TypePowerControl, proto.PowerControlPayload{
		ReqID: reqID, Action: action,
	})
	if err != nil {
		writeJSON(w, statusForRoundTrip(err), map[string]any{"ok": false, "error": err.Error()})
		return
	}

	var res proto.PowerControlResultPayload
	if err := proto.As(env, &res); err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]any{"ok": false, "error": "bad agent response"})
		return
	}
	log.Printf("power: agent=%s action=%s ok=%v err=%q", agentID, action, res.OK, res.Error)
	writeJSON(w, http.StatusOK, map[string]any{"ok": res.OK, "error": res.Error, "action": res.Action})
}

// statusForRoundTrip maps a round-trip error to an HTTP status.
func statusForRoundTrip(err error) int {
	if errors.Is(err, errRoundTripTimeout) {
		return http.StatusGatewayTimeout
	}
	return http.StatusNotFound
}

// sanitizeFilename strips path separators and quotes from a download filename
// so the Content-Disposition header stays well-formed.
func sanitizeFilename(name string) string {
	name = strings.ReplaceAll(name, "\"", "")
	name = strings.ReplaceAll(name, "\\", "")
	name = strings.ReplaceAll(name, "/", "")
	if name == "" {
		return "download"
	}
	return name
}
