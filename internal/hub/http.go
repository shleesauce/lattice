package hub

import (
	"encoding/json"
	"io/fs"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/gorilla/websocket"

	"github.com/dylanstoryyy/lattice/internal/proto"
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  4096,
	WriteBufferSize: 4096,
	// Phase 1: dashboard and agents may live on other origins; allow all.
	CheckOrigin: func(r *http.Request) bool { return true },
}

// routes wires the HTTP handlers: REST under /api, the two WS endpoints, and
// the embedded dashboard with SPA fallback for everything else.
func (h *Hub) routes() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("/api/health", h.handleHealth)
	mux.HandleFunc("/api/fleet", h.handleFleet)
	mux.HandleFunc("/api/agents/", h.handleAgentSub)

	mux.HandleFunc("/ws/agent", h.handleAgentWS)
	mux.HandleFunc("/ws/dashboard", h.handleDashboardWS)

	mux.Handle("/", h.staticHandler())
	return mux
}

func (h *Hub) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":      true,
		"version": h.version,
		"agents":  len(h.registry.snapshot(offlineAfter)),
	})
}

func (h *Hub) handleFleet(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"agents": h.registry.snapshot(offlineAfter),
	})
}

// handleAgentSub routes /api/agents/{id}/exec.
func (h *Hub) handleAgentSub(w http.ResponseWriter, r *http.Request) {
	rest := strings.TrimPrefix(r.URL.Path, "/api/agents/")
	id, action, ok := strings.Cut(rest, "/")
	if !ok || action != "exec" || id == "" {
		http.NotFound(w, r)
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	h.handleExec(w, r, id)
}

func (h *Hub) handleExec(w http.ResponseWriter, r *http.Request, agentID string) {
	var body struct {
		Command string `json:"command"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid json body", http.StatusBadRequest)
		return
	}
	if strings.TrimSpace(body.Command) == "" {
		http.Error(w, "command is required", http.StatusBadRequest)
		return
	}

	conn, ok := h.registry.getAgent(agentID)
	if !ok {
		http.Error(w, "agent not connected", http.StatusNotFound)
		return
	}
	if !conn.isLive(offlineAfter) {
		// Connection still in the map but past its heartbeat window: the command
		// would hang "running" forever on the dashboard. Reject instead.
		http.Error(w, "agent offline", http.StatusNotFound)
		return
	}

	cmdID := newCmdID()
	if err := h.store.InsertCommandStarted(cmdID, agentID, body.Command, time.Now()); err != nil {
		log.Printf("exec: persist start failed: %v", err)
	}
	if err := conn.send(proto.TypeRunCommand, proto.RunCommandPayload{CmdID: cmdID, Command: body.Command}); err != nil {
		http.Error(w, "failed to dispatch to agent", http.StatusBadGateway)
		return
	}

	log.Printf("exec: agent=%s cmd=%s %q", agentID, cmdID, body.Command)
	writeJSON(w, http.StatusAccepted, map[string]any{"cmdId": cmdID})
}

// staticHandler serves the embedded dashboard with SPA fallback to index.html
// for unknown paths that are not under /api or /ws.
func (h *Hub) staticHandler() http.Handler {
	sub, err := DashboardFS()
	if err != nil {
		log.Printf("dashboard fs unavailable: %v", err)
		return http.NotFoundHandler()
	}
	fileServer := http.FileServerFS(sub)

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := strings.TrimPrefix(r.URL.Path, "/")
		if p == "" {
			fileServer.ServeHTTP(w, r)
			return
		}
		if _, statErr := fs.Stat(sub, p); statErr != nil {
			// SPA fallback: serve index.html for unknown app routes.
			r2 := r.Clone(r.Context())
			r2.URL.Path = "/"
			fileServer.ServeHTTP(w, r2)
			return
		}
		fileServer.ServeHTTP(w, r)
	})
}

func writeJSON(w http.ResponseWriter, status int, obj any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(obj)
}
