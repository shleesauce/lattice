package hub

import (
	"encoding/json"
	"io/fs"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/gorilla/websocket"

	"github.com/shleesauce/lattice/internal/proto"
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  4096,
	WriteBufferSize: 4096,
	// Phase 3: same-origin check to block cross-site WebSocket hijacking. Native
	// clients (the agent, curl) send no Origin header → allowed. A browser sends
	// Origin, which must match the host the hub was reached on; a page on any
	// other origin (an attacker's site driving the operator's browser) is rejected.
	CheckOrigin: checkSameOrigin,
}

// checkSameOrigin allows requests with no Origin (native clients) and otherwise
// requires the Origin's host to equal the request host.
func checkSameOrigin(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return true
	}
	u, err := url.Parse(origin)
	if err != nil {
		return false
	}
	return strings.EqualFold(u.Host, r.Host)
}

// routes wires the HTTP handlers: REST under /api, the two WS endpoints, and
// the embedded dashboard with SPA fallback for everything else.
func (h *Hub) routes() http.Handler {
	mux := http.NewServeMux()

	// OPEN (never gated): liveness, setup wizard, auth endpoints. /api/health in
	// particular MUST stay open — the fleet watchdog + version tooling curl it
	// unauthenticated.
	mux.HandleFunc("/api/health", h.handleHealth)

	// GATED (admin auth required when a password hash is configured; see auth.go).
	mux.HandleFunc("/api/fleet", h.requireAuth(h.handleFleet))
	mux.HandleFunc("/api/devices", h.requireAuth(h.handleDevices))
	mux.HandleFunc("/api/enroll", h.requireAuth(h.handleEnroll))
	// Phase 4: per-machine revocable enrollment tokens (admin ops). Distinct mux
	// patterns from /api/enroll: the exact /api/enroll/tokens lists/mints, and the
	// /api/enroll/tokens/ prefix parses {token}/revoke.
	mux.HandleFunc("/api/enroll/tokens", h.requireAuth(h.handleEnrollTokens))
	mux.HandleFunc("/api/enroll/tokens/", h.requireAuth(h.handleEnrollTokenItem))
	mux.HandleFunc("/api/agents/", h.requireAuth(h.handleAgentSub))

	// Phase 3: workspace (projects → sessions, placement, audit, settings) — gated.
	mux.HandleFunc("/api/projects", h.requireAuth(h.handleProjects))
	mux.HandleFunc("/api/sessions", h.requireAuth(h.handleSessions))
	mux.HandleFunc("/api/sessions/", h.requireAuth(h.handleSessions))
	mux.HandleFunc("/api/placement", h.requireAuth(h.handlePlacement))
	mux.HandleFunc("/api/settings", h.requireAuth(h.handleSettings))
	// Release notes + update check (v0.1.5): recent GitHub releases with the running
	// build flagged. Admin-gated like the rest of the workspace API.
	mux.HandleFunc("/api/releases", h.requireAuth(h.handleReleases))

	// Fire-and-forget approve/deny (v0.1.5) — OPEN by design: the unguessable
	// single-use nonce in the path is a capability credential (see handleApproval).
	// The phone tapping the ntfy action button carries no admin token, so this must
	// NOT be admin-gated; the nonce alone authorizes the one keystroke it injects.
	mux.HandleFunc("/api/approvals/", h.handleApproval)

	// Claude Code hook callbacks (C, v0.1.5) — OPEN by design like /api/approvals:
	// the per-session HookToken in the body is the capability credential. The hook
	// script runs on the agent box with no admin token, so this must NOT be
	// admin-gated; the token alone authorizes the precise state edge it reports.
	mux.HandleFunc("/api/hooks/state", h.handleHookState)

	// Phase 2: first-run setup wizard (unauthenticated; gated 409 once complete).
	mux.HandleFunc("/api/setup/status", h.handleSetupStatus)
	mux.HandleFunc("/api/setup/check-root", h.handleSetupCheckRoot)
	mux.HandleFunc("/api/setup", h.handleSetup)

	// Phase 3: admin auth endpoints (open: these are how you obtain a session).
	mux.HandleFunc("/api/auth/status", h.handleAuthStatus)
	mux.HandleFunc("/api/auth/login", h.handleAuthLogin)
	mux.HandleFunc("/api/auth/logout", h.handleAuthLogout)

	// Packaging / enrollment (Phase 4): binary distribution + installers (open —
	// bootstrap surfaces a new machine must reach before it has any credential).
	mux.HandleFunc("/dl/", h.handleDownloadBinary)
	mux.HandleFunc("/install.sh", h.handleInstallSh)
	mux.HandleFunc("/install.ps1", h.handleInstallPs1)

	// TOKEN-GATED agent surfaces: already authenticated by the enrollment token
	// inside the handler (register frame / ?token=), NOT admin auth. Do not wrap.
	mux.HandleFunc("/ws/agent", h.handleAgentWS)
	mux.HandleFunc("/ws/tunnel", h.handleTunnelWS) // IDE: agent's 2nd dial-out (yamux editor tunnel, D27)

	// Browser WS surfaces — admin-gated like the REST API.
	mux.HandleFunc("/ws/dashboard", h.requireAuth(h.handleDashboardWS))
	mux.HandleFunc("/ws/terminal", h.requireAuth(h.handleTerminalWS))
	mux.HandleFunc("/ws/session", h.requireAuth(h.handleSessionWS))

	// IDE: reverse-proxy an agent's embedded code-server over the tunnel (D27).
	// /editor/{sessionId}/* — the trailing wildcard captures all workbench assets.
	mux.HandleFunc("/editor/", h.requireAuth(h.handleEditorProxy))

	// Preview: reverse-proxy an agent's dev server over the same tunnel (D32).
	// /preview/{agentId}/{port}/* — works for any localhost dev server on the
	// machine, from any device that can reach the hub (phone included).
	mux.HandleFunc("/preview/", h.requireAuth(h.handlePreviewProxy))

	mux.Handle("/", h.staticHandler())
	return mux
}

// handleHealth is the UNAUTHENTICATED liveness probe (curled by fleet-watchdog.sh
// and version tooling). It must be cheap: the watchdog hits it every sweep and
// only cares that the hub answers 200, so it returns a fixed payload plus an
// in-memory connected-agent count — NOT fleet(), which does up to 3 reads on the
// single SQLite connection and let a health-hammer starve that conn. "agents" is
// now the count of LIVE (connected) agents from the registry, not the
// online+offline union fleet() returns; that's fine for a liveness gate.
func (h *Hub) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":      true,
		"version": h.version,
		"agents":  h.registry.liveAgentCount(),
	})
}

func (h *Hub) handleFleet(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"agents": h.fleet(),
	})
}

// handleDevices returns the unified fleet: lattice agents merged with Tailscale
// peers and SSH-config hosts, deduped per physical machine. This is the
// superset the dashboard fleet map renders — including phones and machines that
// don't run the lattice agent.
func (h *Hub) handleDevices(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"devices": h.devices(),
	})
}

// handleAgentSub routes the /api/agents/{id}/{action} subtree.
func (h *Hub) handleAgentSub(w http.ResponseWriter, r *http.Request) {
	rest := strings.TrimPrefix(r.URL.Path, "/api/agents/")
	id, action, ok := strings.Cut(rest, "/")
	if !ok || id == "" {
		http.NotFound(w, r)
		return
	}

	switch action {
	case "exec":
		if requireMethod(w, r, http.MethodPost) {
			h.handleExec(w, r, id)
		}
	case "files":
		if requireMethod(w, r, http.MethodGet) {
			h.handleFiles(w, r, id)
		}
	case "download":
		if requireMethod(w, r, http.MethodGet) {
			h.handleDownload(w, r, id)
		}
	case "wake":
		if requireMethod(w, r, http.MethodPost) {
			h.handleWake(w, r, id)
		}
	case "rename":
		if requireMethod(w, r, http.MethodPost) {
			h.handleAgentRename(w, r, id)
		}
	case "remove":
		if requireMethod(w, r, http.MethodPost) {
			h.handleAgentRemove(w, r, id)
		}
	default:
		http.NotFound(w, r)
	}
}

// requireMethod reports whether r used the wanted HTTP method. On a mismatch it
// writes a 405 and returns false, so callers can guard a handler with a single
// `if requireMethod(...) { ... }`.
func requireMethod(w http.ResponseWriter, r *http.Request, method string) bool {
	if r.Method != method {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return false
	}
	return true
}

// handleAgentRename sets a human display-name override for an agent. The label is
// stored separately from the agents row so it survives the agent's re-register
// (which UPSERTs Name back to the hostname); fleet() overlays it onto Agent.Name.
func (h *Hub) handleAgentRename(w http.ResponseWriter, r *http.Request, agentID string) {
	var body struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid json body", http.StatusBadRequest)
		return
	}
	name := strings.TrimSpace(body.Name)
	if len(name) < 1 || len(name) > 40 {
		http.Error(w, "name must be 1-40 characters", http.StatusBadRequest)
		return
	}
	if err := h.store.SetAgentLabel(agentID, name); err != nil {
		log.Printf("rename: set label failed: %v", err)
		http.Error(w, "failed to rename", http.StatusInternalServerError)
		return
	}
	h.broadcastFleet()
	log.Printf("agent rename: id=%s name=%q", agentID, name)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "id": agentID, "name": name})
}

// handleAgentRemove forgets an agent: closes its live connection if any, orphans
// its sessions, deletes the identity row + label, and revokes the per-machine
// enroll token it used (if any). A removed box that re-enrolls with the MASTER
// token reappears — that's expected; only its per-machine token is revoked.
func (h *Hub) handleAgentRemove(w http.ResponseWriter, r *http.Request, agentID string) {
	if conn, ok := h.registry.getAgent(agentID); ok {
		// Drop the live socket so the box stops checking in under this id; its read
		// loop unwinds and the deferred cleanup runs (orphan + broadcast) too.
		conn.conn.Close()
	}
	if err := h.store.MarkAgentSessionsOrphaned(agentID); err != nil {
		log.Printf("remove: orphan sessions failed: %v", err)
	}
	if err := h.store.RevokeEnrollTokenForAgent(agentID); err != nil {
		log.Printf("remove: revoke enroll token failed: %v", err)
	}
	if err := h.store.DeleteAgentLabel(agentID); err != nil {
		log.Printf("remove: delete label failed: %v", err)
	}
	if err := h.store.DeleteAgent(agentID); err != nil {
		log.Printf("remove: delete agent failed: %v", err)
		http.Error(w, "failed to remove", http.StatusInternalServerError)
		return
	}
	h.broadcastFleet()
	log.Printf("agent remove: id=%s", agentID)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
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

	// Reject if the agent is gone OR still mapped but past its heartbeat window —
	// otherwise the command would hang "running" forever on the dashboard.
	conn, ok := h.registry.liveAgent(agentID)
	if !ok {
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

	// index.html must ALWAYS revalidate so a fresh build's content-hashed asset
	// names are picked up immediately — otherwise a browser heuristically caches a
	// stale index.html that points at an old bundle and the user is frozen on the
	// previous build (the "I rebuilt but the UI didn't change / a fixed control is
	// still broken" trap). Vite emits everything under assets/ with a content hash
	// in the filename, so those are safe to cache forever (immutable).
	serveIndex := func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-cache")
		r2 := r.Clone(r.Context())
		r2.URL.Path = "/"
		fileServer.ServeHTTP(w, r2)
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := strings.TrimPrefix(r.URL.Path, "/")
		if p == "" {
			serveIndex(w, r)
			return
		}
		if _, statErr := fs.Stat(sub, p); statErr != nil {
			// SPA fallback: serve index.html for unknown app routes.
			serveIndex(w, r)
			return
		}
		if strings.HasPrefix(p, "assets/") {
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		} else {
			// Root-level files (favicon, logo, manifest) can change build-to-build
			// without a hash, so make them revalidate too.
			w.Header().Set("Cache-Control", "no-cache")
		}
		fileServer.ServeHTTP(w, r)
	})
}

func writeJSON(w http.ResponseWriter, status int, obj any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(obj)
}
