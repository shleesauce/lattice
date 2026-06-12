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

// routePolicy is the authorization a route requires. Centralizing this in a table
// (rather than wrapping each handler by hand at its mux.HandleFunc) makes the whole
// auth posture readable in one place AND means a new route can't silently ship
// ungated — the wiring loop forces every entry to name a policy. The post-ship audit's
// HIGH passwordless-hub holes existed precisely because privilege was wired per-handler
// and three sibling handlers were missed; this is the prevent-recurrence seam.
type routePolicy int

const (
	// policyOpen: no credential. Bootstrap/liveness/capability-nonce surfaces a peer
	// must reach before (or without) an admin session. A handler so marked is trusted
	// to self-gate if it needs to (e.g. handleSetup → setupAllowed).
	policyOpen routePolicy = iota
	// policyAuth: admin session/Bearer when a password hash is set; pass-through on a
	// passwordless hub (the legacy "open on a trusted network" mode).
	policyAuth
	// policyAuthOrToken: like policyAuth, but ALSO demands the master token on a
	// passwordless hub (for credential-bearing endpoints that hand out / mint secrets).
	policyAuthOrToken
	// policyPrivileged: RCE/destructive — the master token is required even on a
	// passwordless hub. Composes admin-auth (when set) with the fail-closed token gate.
	policyPrivileged
	// policyTokenGated: authenticated INSIDE the handler by an enrollment token (the
	// register frame / ?token=), not admin auth. Must never be admin-wrapped.
	policyTokenGated
)

// gate turns a routePolicy into the middleware wrapper for a handler. Fails closed on
// an unrecognized policy so a future enum value can't accidentally expose a route.
func (h *Hub) gate(p routePolicy, fn http.HandlerFunc) http.HandlerFunc {
	switch p {
	case policyOpen, policyTokenGated:
		return fn
	case policyAuth:
		return h.requireAuth(fn)
	case policyAuthOrToken:
		return h.requireAuthOrToken(fn)
	case policyPrivileged:
		// Admin-auth (a no-op pass on a passwordless hub) THEN the fail-closed
		// privilege check — requirePrivileged assumes requireAuth already ran.
		return h.requireAuth(func(w http.ResponseWriter, r *http.Request) {
			if !h.requirePrivileged(w, r) {
				return
			}
			fn(w, r)
		})
	default:
		return func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, "forbidden: unspecified route policy", http.StatusForbidden)
		}
	}
}

// routes wires the HTTP handlers: REST under /api, the two WS endpoints, and
// the embedded dashboard with SPA fallback for everything else.
func (h *Hub) routes() http.Handler {
	mux := http.NewServeMux()

	// The complete authorization posture, one row per route (see routePolicy). Every
	// new route must add a row here and pick a policy — there is no ungated default.
	for _, rt := range []struct {
		pattern string
		policy  routePolicy
		handler http.HandlerFunc
	}{
		// OPEN — no credential. /api/health is curled unauthenticated by the fleet
		// watchdog + version tooling. The capability endpoints carry their own
		// credential in the path/body (nonce / HookToken), NOT an admin session, so
		// the phone/agent that hits them with no admin token must not be blocked.
		{"/api/health", policyOpen, h.handleHealth},
		{"/api/approvals/", policyOpen, h.handleApproval},   // single-use nonce in path = the credential
		{"/api/hooks/state", policyOpen, h.handleHookState}, // per-session HookToken in body = the credential
		{"/api/setup/status", policyOpen, h.handleSetupStatus},
		{"/api/setup/check-root", policyOpen, h.handleSetupCheckRoot},
		{"/api/setup", policyOpen, h.handleSetup}, // self-gates via setupAllowed (loopback OR token)
		{"/api/auth/status", policyOpen, h.handleAuthStatus},
		{"/api/auth/login", policyOpen, h.handleAuthLogin},
		{"/api/auth/logout", policyOpen, h.handleAuthLogout},
		{"/dl/", policyOpen, h.handleDownloadBinary},   // bootstrap: a new box has no credential yet
		{"/install.sh", policyOpen, h.handleInstallSh}, // "
		{"/install.ps1", policyOpen, h.handleInstallPs1},

		// AUTH — admin session/Bearer when a password is set; pass-through otherwise.
		{"/api/fleet", policyAuth, h.handleFleet},
		{"/api/devices", policyAuth, h.handleDevices},
		{"/api/enroll", policyAuth, h.handleEnroll},    // self-tightens to token via requireAuthOrToken inside
		{"/api/agents/", policyAuth, h.handleAgentSub}, // per-ACTION privilege enforced in handleAgentSub
		{"/api/projects", policyAuth, h.handleProjects},
		{"/api/sessions", policyAuth, h.handleSessions},
		{"/api/sessions/", policyAuth, h.handleSessions},
		{"/api/workflows", policyAuth, h.handleWorkflows},
		{"/api/placement", policyAuth, h.handlePlacement},
		{"/api/settings", policyAuth, h.handleSettings},
		{"/api/releases", policyAuth, h.handleReleases},
		{"/ws/dashboard", policyAuth, h.handleDashboardWS},
		{"/ws/terminal", policyAuth, h.handleTerminalWS},
		{"/ws/session", policyAuth, h.handleSessionWS},
		{"/editor/", policyAuth, h.handleEditorProxy},             // code-server over the tunnel (D27)
		{"/preview/", policyAuth, h.handlePreviewProxy},           // dev server, STRIP mode (D32)
		{"/fpreview/", policyAuth, h.handleFrameworkPreviewProxy}, // dev server, NO-STRIP (Vite/Next)

		// AUTH-OR-TOKEN — credential-bearing: master token even on a passwordless hub.
		// These list/mint fleet enrollment secrets, so they must not ride auth-off.
		{"/api/enroll/tokens", policyAuthOrToken, h.handleEnrollTokens},
		{"/api/enroll/tokens/", policyAuthOrToken, h.handleEnrollTokenItem},

		// PRIVILEGED — RCE/destructive: master token even on a passwordless hub.
		{"/api/update", policyPrivileged, h.handleUpdate}, // swaps every machine's binary fleet-wide

		// TOKEN-GATED — authenticated inside the handler by the enrollment token
		// (register frame / ?token=), NOT admin auth. Must NOT be admin-wrapped.
		{"/ws/agent", policyTokenGated, h.handleAgentWS},
		{"/ws/tunnel", policyTokenGated, h.handleTunnelWS}, // agent's 2nd dial-out (yamux editor tunnel, D27)
	} {
		mux.HandleFunc(rt.pattern, h.gate(rt.policy, rt.handler))
	}

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
	case "power":
		if requireMethod(w, r, http.MethodPost) {
			h.handlePower(w, r, id)
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
	// Privilege-class: disconnects an agent, orphans its sessions, and revokes its
	// enroll token. Fail closed on a passwordless hub (see requirePrivileged) so a
	// tailnet peer can't loop it to disconnect agents and revoke tokens — a
	// persistent fleet DoS.
	if !h.requirePrivileged(w, r) {
		return
	}
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
	// RCE-class: fail closed even on a passwordless hub (see requirePrivileged).
	if !h.requirePrivileged(w, r) {
		return
	}
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
