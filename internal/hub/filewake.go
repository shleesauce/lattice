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

	"github.com/dylanstoryyy/lattice/internal/proto"
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
	ac, ok := h.registry.getAgent(agentID)
	if !ok || !ac.isLive(offlineAfter) {
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
	case <-time.After(pendingTimeout):
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
	if res.Error != "" {
		writeJSON(w, http.StatusOK, res) // path-level error carried in payload
		return
	}
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
func (h *Hub) handleWake(w http.ResponseWriter, r *http.Request, senderID string) {
	var body struct {
		MAC string `json:"mac"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "invalid json body"})
		return
	}
	if strings.TrimSpace(body.MAC) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "mac is required"})
		return
	}

	reqID := newReqID()
	env, err := h.roundTrip(senderID, reqID, proto.TypeWake, proto.WakePayload{
		ReqID: reqID, MAC: body.MAC,
	})
	if err != nil {
		writeJSON(w, statusForRoundTrip(err), map[string]any{"ok": false, "error": err.Error()})
		return
	}

	var res proto.WakeResultPayload
	if err := proto.As(env, &res); err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]any{"ok": false, "error": "bad agent response"})
		return
	}
	log.Printf("wake: sender=%s mac=%s ok=%v err=%q", senderID, body.MAC, res.OK, res.Error)
	writeJSON(w, http.StatusOK, map[string]any{"ok": res.OK, "error": res.Error})
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
