package hub

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/dylanstoryyy/lattice/internal/proto"
)

// handleProjects routes /api/projects: GET lists the workspace projects,
// POST scaffolds and onboards a brand-new one (see projectcreate.go).
func (h *Hub) handleProjects(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		h.handleListProjects(w, r)
	case http.MethodPost:
		h.handleCreateProject(w, r)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// handleListProjects answers GET /api/projects with the directories under the
// configured projects root (the Syncthing-synced ~/AI-Hub/projects). Dotfiles
// and plain files are skipped.
func (h *Hub) handleListProjects(w http.ResponseWriter, r *http.Request) {
	entries, err := os.ReadDir(h.projectsRoot)
	if err != nil {
		// An absent root is not an error to the caller — just no projects yet.
		writeJSON(w, http.StatusOK, map[string]any{"projects": []any{}})
		return
	}
	type project struct {
		Name string `json:"name"`
		Path string `json:"path"`
	}
	out := make([]project, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		out = append(out, project{Name: e.Name(), Path: filepath.Join(h.projectsRoot, e.Name())})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	writeJSON(w, http.StatusOK, map[string]any{"projects": out})
}

// handleSessions routes /api/sessions and /api/sessions/{id}/...
func (h *Hub) handleSessions(w http.ResponseWriter, r *http.Request) {
	rest := strings.TrimPrefix(r.URL.Path, "/api/sessions")
	rest = strings.TrimPrefix(rest, "/")

	switch {
	case rest == "":
		switch r.Method {
		case http.MethodGet:
			h.handleListSessions(w, r)
		case http.MethodPost:
			h.handleCreateSession(w, r)
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	default:
		id, action, _ := strings.Cut(rest, "/")
		switch {
		case action == "" && r.Method == http.MethodDelete:
			h.handleDeleteSession(w, r, id)
		case action == "" && r.Method == http.MethodPatch:
			h.handleUpdateSession(w, r, id)
		case action == "resume" && r.Method == http.MethodPost:
			h.handleResumeSession(w, r, id)
		default:
			http.NotFound(w, r)
		}
	}
}

func (h *Hub) handleListSessions(w http.ResponseWriter, r *http.Request) {
	recs, err := h.store.ListSessions()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	out := make([]sessionView, 0, len(recs))
	for _, rec := range recs {
		out = append(out, toSessionView(rec))
	}
	writeJSON(w, http.StatusOK, map[string]any{"sessions": out})
}

// createSessionBody is the POST /api/sessions request body. Scope is "project"
// (default — a synced project, auto-placeable) or "device" (machine-local work
// pinned to PinAgentId, cwd = that device's home).
type createSessionBody struct {
	Kind        string `json:"kind"`
	Scope       string `json:"scope"`
	ProjectPath string `json:"projectPath"`
	Title       string `json:"title"`
	UserAgentID string `json:"userAgentId"`
	PinAgentID  string `json:"pinAgentId"`
}

func (h *Hub) handleCreateSession(w http.ResponseWriter, r *http.Request) {
	var body createSessionBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid json body"})
		return
	}
	kind := proto.SessionKind(strings.TrimSpace(body.Kind))
	if kind != proto.SessionTerminal && kind != proto.SessionClaude && kind != proto.SessionEditor {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "kind must be terminal, claude, or editor"})
		return
	}
	scope := strings.TrimSpace(body.Scope)
	if scope == "" {
		scope = "project"
	}

	// A device session is pinned to one machine and runs in that machine's home
	// (empty projectPath ⇒ the agent resolves home). A project session needs a path.
	projectPath := strings.TrimSpace(body.ProjectPath)
	if scope == "device" {
		if strings.TrimSpace(body.PinAgentID) == "" {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": "pinAgentId is required for a device session"})
			return
		}
		projectPath = "" // home, resolved on the agent
	} else if projectPath == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "projectPath is required"})
		return
	}

	req := PlacementRequest{
		Kind:        kind,
		ProjectPath: projectPath,
		UserAgentID: body.UserAgentID,
		PinAgentID:  body.PinAgentID,
	}
	placement := ScorePlacement(req, h.fleet(), time.Now())
	if placement.Chosen == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error":     "no eligible agent for this session",
			"placement": placement,
		})
		return
	}
	// A device session must run on its device or fail — never silently fall back
	// to another machine (defeats the point of acting ON that box).
	if scope == "device" && placement.Chosen != body.PinAgentID {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error":     "this device can't host the session: " + deviceExcludeReason(placement, body.PinAgentID),
			"placement": placement,
		})
		return
	}

	rec, err := h.createOnAgent(req.Kind, scope, projectPath, body.Title, placement.Chosen, "")
	if err != nil {
		writeJSON(w, statusForRoundTrip(err), map[string]any{"error": err.Error(), "placement": placement})
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{
		"session":   toSessionView(rec),
		"placement": placement,
	})
}

// deviceExcludeReason pulls the placement exclusion reason for a pinned device so
// the UI can say WHY (e.g. "claude not installed", "offline").
func deviceExcludeReason(p PlacementResult, agentID string) string {
	for _, c := range p.Candidates {
		if c.AgentID == agentID {
			if c.Excluded != "" {
				return c.Excluded
			}
			return "not eligible"
		}
	}
	return "device not found"
}

// createOnAgent allocates the session row, dispatches session_create to the
// chosen agent, and flips the row to live on ack. resumeID is non-empty only for
// a resume onto a (possibly different) agent. The session row id is reused on
// resume so the logical conversation keeps one identity (D20).
func (h *Hub) createOnAgent(kind proto.SessionKind, scope, projectPath, title, agentID, resumeID string) (SessionRecord, error) {
	now := time.Now()
	sessionID := resumeID
	isResume := resumeID != ""
	if !isResume {
		sessionID = newSessionID()
	}
	if scope == "" {
		scope = "project"
	}

	// Approval kill switch (D21): skip permissions unless approval is forced.
	skipPerms := kind == proto.SessionClaude && !h.forceApproval(agentID)

	rec := SessionRecord{
		ID:           sessionID,
		ProjectPath:  projectPath,
		Kind:         string(kind),
		AgentID:      agentID,
		Title:        title,
		Status:       proto.SessionStarting,
		Scope:        scope,
		CreatedAt:    now,
		LastActiveAt: now,
	}
	if kind == proto.SessionClaude {
		rec.ClaudeSessionID = sessionID
	}
	if err := h.store.UpsertSession(rec); err != nil {
		return SessionRecord{}, err
	}

	reqID := newReqID()
	create := proto.SessionCreatePayload{
		ReqID:     reqID,
		SessionID: sessionID,
		Kind:      kind,
		Cwd:       projectPath,
		SkipPerms: skipPerms,
	}
	if isResume {
		create.ResumeID = resumeID
	}

	env, err := h.roundTrip(agentID, reqID, proto.TypeSessionCreate, create)
	if err != nil {
		_ = h.store.UpdateSessionStatus(sessionID, proto.SessionExited, time.Now())
		return SessionRecord{}, err
	}
	var ack proto.SessionCreatedPayload
	if err := proto.As(env, &ack); err != nil {
		_ = h.store.UpdateSessionStatus(sessionID, proto.SessionExited, time.Now())
		return SessionRecord{}, err
	}
	if ack.Error != "" {
		_ = h.store.UpdateSessionStatus(sessionID, proto.SessionExited, time.Now())
		return SessionRecord{}, errFromAgent(ack.Error)
	}

	if err := h.store.SetSessionAgent(sessionID, agentID, ack.ClaudeSessionID); err != nil {
		return SessionRecord{}, err
	}
	if err := h.store.UpdateSessionStatus(sessionID, proto.SessionLive, time.Now()); err != nil {
		return SessionRecord{}, err
	}
	out, _, _ := h.store.GetSession(sessionID)
	return out, nil
}

func (h *Hub) handleDeleteSession(w http.ResponseWriter, r *http.Request, id string) {
	rec, ok, err := h.store.GetSession(id)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	if !ok {
		http.NotFound(w, r)
		return
	}
	if ac, online := h.registry.getAgent(rec.AgentID); online {
		_ = ac.send(proto.TypeSessionClose, proto.SessionControlPayload{SessionID: id})
	}
	if err := h.store.UpdateSessionStatus(id, proto.SessionExited, time.Now()); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	// ?purge=1 hard-removes the row (true delete) after ending the process;
	// without it, delete is soft (process ends, row kept as exited).
	if r.URL.Query().Get("purge") == "1" {
		if err := h.store.DeleteSessionRow(id); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
			return
		}
	}
	h.broadcastSessions()
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// handleUpdateSession patches mutable session fields. Currently supports
// {archived} to hide/restore a session from the active workspace tree.
func (h *Hub) handleUpdateSession(w http.ResponseWriter, r *http.Request, id string) {
	var body struct {
		Archived *bool `json:"archived"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid json body", http.StatusBadRequest)
		return
	}
	rec, ok, err := h.store.GetSession(id)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	if !ok {
		http.NotFound(w, r)
		return
	}
	if body.Archived != nil {
		if err := h.store.SetSessionArchived(id, *body.Archived, time.Now()); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
			return
		}
		rec.Archived = *body.Archived
	}
	h.broadcastSessions()
	writeJSON(w, http.StatusOK, toSessionView(rec))
}

// resumeBody is the POST /api/sessions/{id}/resume body.
type resumeBody struct {
	UserAgentID string `json:"userAgentId"`
	PinAgentID  string `json:"pinAgentId"`
}

// handleResumeSession resumes a claude session (D20): re-place, then create on
// the new agent with --resume <claudeSessionId>, reusing the same row id.
func (h *Hub) handleResumeSession(w http.ResponseWriter, r *http.Request, id string) {
	rec, ok, err := h.store.GetSession(id)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	if !ok {
		http.NotFound(w, r)
		return
	}
	if proto.SessionKind(rec.Kind) != proto.SessionClaude {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "only claude sessions are resumable"})
		return
	}

	var body resumeBody
	_ = json.NewDecoder(r.Body).Decode(&body)

	// A device session is bound to its machine — resume it there, not elsewhere.
	pin := body.PinAgentID
	if rec.Scope == "device" {
		pin = rec.AgentID
	}
	req := PlacementRequest{
		Kind:        proto.SessionClaude,
		ProjectPath: rec.ProjectPath,
		UserAgentID: body.UserAgentID,
		PinAgentID:  pin,
	}
	placement := ScorePlacement(req, h.fleet(), time.Now())
	if placement.Chosen == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error": "no eligible agent to resume on", "placement": placement,
		})
		return
	}

	resumeID := rec.ClaudeSessionID
	if resumeID == "" {
		resumeID = rec.ID
	}
	out, err := h.createOnAgent(proto.SessionClaude, rec.Scope, rec.ProjectPath, rec.Title, placement.Chosen, resumeID)
	if err != nil {
		writeJSON(w, statusForRoundTrip(err), map[string]any{"error": err.Error(), "placement": placement})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"session": toSessionView(out), "placement": placement})
}

// handlePlacement answers POST /api/placement with a placement preview (no side
// effects) so the UI can show the ranked machines before committing.
func (h *Hub) handlePlacement(w http.ResponseWriter, r *http.Request) {
	var body createSessionBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid json body"})
		return
	}
	kind := proto.SessionKind(strings.TrimSpace(body.Kind))
	if kind != proto.SessionTerminal && kind != proto.SessionClaude && kind != proto.SessionEditor {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "kind must be terminal, claude, or editor"})
		return
	}
	result := ScorePlacement(PlacementRequest{
		Kind:        kind,
		ProjectPath: body.ProjectPath,
		UserAgentID: body.UserAgentID,
		PinAgentID:  body.PinAgentID,
	}, h.fleet(), time.Now())
	writeJSON(w, http.StatusOK, result)
}

// handleAudit answers GET /api/audit?session=<id> with the session's audit trail.
func (h *Hub) handleAudit(w http.ResponseWriter, r *http.Request) {
	sessionID := r.URL.Query().Get("session")
	if sessionID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "session is required"})
		return
	}
	entries, err := h.store.ListAudit(sessionID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	if entries == nil {
		entries = []AuditEntry{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"audit": entries})
}

// handleSettings reads (GET) or writes (POST) the approval kill-switch toggles
// (D21). POST body: {"key":"force_approval_global"|"force_approval:<agentId>",
// "value":"true"|"false"}.
func (h *Hub) handleSettings(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		global, _, _ := h.store.GetSetting("force_approval_global")
		writeJSON(w, http.StatusOK, map[string]any{
			"forceApprovalGlobal": isTrue(global),
		})
	case http.MethodPost:
		var body struct {
			Key   string `json:"key"`
			Value string `json:"value"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid json body"})
			return
		}
		if !isApprovalKey(body.Key) {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": "unsupported settings key"})
			return
		}
		if err := h.store.SetSetting(body.Key, body.Value); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// isApprovalKey whitelists the settable approval keys so arbitrary settings can't
// be written through the public endpoint.
func isApprovalKey(key string) bool {
	return key == "force_approval_global" || strings.HasPrefix(key, "force_approval:")
}
