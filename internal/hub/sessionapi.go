package hub

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/shleesauce/lattice/internal/proto"
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
// configured projects root. Dotfiles and plain files are skipped.
func (h *Hub) handleListProjects(w http.ResponseWriter, r *http.Request) {
	root := h.ProjectsRoot()
	entries, err := os.ReadDir(root)
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
		out = append(out, project{Name: e.Name(), Path: filepath.Join(root, e.Name())})
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
	case rest == "trash" && r.Method == http.MethodDelete:
		// DELETE /api/sessions/trash — empty Trash now (purge all trashed rows).
		h.handleEmptyTrash(w, r)
	default:
		id, action, _ := strings.Cut(rest, "/")
		switch {
		case action == "" && r.Method == http.MethodDelete:
			h.handleDeleteSession(w, r, id)
		case action == "" && r.Method == http.MethodPatch:
			h.handleUpdateSession(w, r, id)
		case action == "resume" && r.Method == http.MethodPost:
			h.handleResumeSession(w, r, id)
		case action == "transcript" && r.Method == http.MethodGet:
			h.handleTranscript(w, r, id)
		case action == "telemetry" && r.Method == http.MethodGet:
			h.handleSessionTelemetry(w, r, id)
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
	Kind           string `json:"kind"`
	Scope          string `json:"scope"`
	ProjectPath    string `json:"projectPath"`
	Title          string `json:"title"`
	UserAgentID    string `json:"userAgentId"`
	PinAgentID     string `json:"pinAgentId"`
	PermissionMode string `json:"permissionMode"` // claude only; agent validates + defaults
	NotifyOnIdle   bool   `json:"notifyOnIdle"`   // claude only; ping my phone when it waits or finishes
	Model          string `json:"model"`          // claude only; full model id (agent validates against allow-list)
	FastMode       bool   `json:"fastMode"`       // claude only; low-effort "fast" launch (--effort low)
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

	// D32: a project session with no explicit pick defaults to the primary
	// coding machine (the Studio) so the 90% case is one click and the session is
	// pinned there for life. An explicit pick always wins; the default is only a
	// soft pin and is ignored when that box is offline/ineligible (placement then
	// falls back to the best eligible host).
	pin := body.PinAgentID
	if scope == "project" && pin == "" {
		pin = h.defaultPin()
	}
	req := PlacementRequest{
		Kind:        kind,
		ProjectPath: projectPath,
		UserAgentID: body.UserAgentID,
		PinAgentID:  pin,
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

	// Only a claude session can go idle/finish in a way worth a phone ping; ignore
	// the flag for terminal/editor so a stray toggle can't arm a never-firing notify.
	notifyOnIdle := body.NotifyOnIdle && req.Kind == proto.SessionClaude
	// Model + fast mode only apply to a claude session; ignore stray values on
	// terminal/editor so a leftover toggle can't alter their launch.
	model := ""
	fastMode := false
	if req.Kind == proto.SessionClaude {
		model = strings.TrimSpace(body.Model)
		fastMode = body.FastMode
	}
	rec, err := h.createOnAgent(req.Kind, scope, projectPath, body.Title, placement.Chosen, "", "", body.PermissionMode, notifyOnIdle, model, fastMode)
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
func (h *Hub) createOnAgent(kind proto.SessionKind, scope, projectPath, title, agentID, resumeID, seedInput, permissionMode string, notifyOnIdle bool, model string, fastMode bool) (SessionRecord, error) {
	now := time.Now()
	sessionID := resumeID
	isResume := resumeID != ""
	if !isResume {
		sessionID = newSessionID()
	}
	if scope == "" {
		scope = "project"
	}

	rec := SessionRecord{
		ID:          sessionID,
		ProjectPath: projectPath,
		Kind:        string(kind),
		AgentID:     agentID,
		Title:       title,
		Status:      proto.SessionStarting,
		// D32: every session is pinned to its chosen device for life — the host is
		// frozen at creation and resume always returns here (see handleResumeSession).
		Pinned:       true,
		Scope:        scope,
		NotifyOnIdle: notifyOnIdle,
		Model:        model,
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
		ReqID:          reqID,
		SessionID:      sessionID,
		Kind:           kind,
		Cwd:            projectPath,
		SeedInput:      seedInput,
		PermissionMode: permissionMode,
		Model:          model,
		FastMode:       fastMode,
	}
	if isResume {
		create.ResumeID = resumeID
	}
	// C (v0.1.5): wire Lattice-managed Claude Code hooks for precise state. Only
	// when (a) it's a claude session and (b) the hub has a canonical URL the
	// on-agent hook script can curl back to. Mint a per-session capability token and
	// ship it + the hub URL so the agent adds `--settings <hooks file>` and injects
	// them into the claude child env. Without a hub URL the agent skips --settings
	// and the hub keeps the PTY-quiet idle heuristic.
	if kind == proto.SessionClaude && h.hooksEnabled() {
		create.HubURL = h.hubURL
		create.HookToken = h.mintHookToken(sessionID, now)
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

// endHostProcess tells the owning agent to terminate a session's live process
// (claude/PTY) and marks it exited, so an archived / trashed / deleted session
// stops eating host RAM+CPU. No-op if the agent is offline (nothing is running).
func (h *Hub) endHostProcess(agentID, sessionID string) {
	// Mark this as a hub-initiated close so the resulting session_exit doesn't fire
	// a "finished" phone ping (the operator closed it on purpose).
	h.approvals.expectExit(sessionID, time.Now())
	if ac, online := h.registry.getAgent(agentID); online {
		_ = ac.send(proto.TypeSessionClose, proto.SessionControlPayload{SessionID: sessionID})
	}
	_ = h.store.UpdateSessionStatus(sessionID, proto.SessionExited, time.Now())
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
	h.endHostProcess(rec.AgentID, id)
	if r.URL.Query().Get("purge") == "1" {
		// Permanent delete (used by "Delete forever" in Trash): drop the row.
		if err := h.store.DeleteSessionRow(id); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
			return
		}
	} else {
		// Default delete = move to Trash: ends the process, hides the session,
		// and auto-purges after the 30-day TTL. Recoverable until then.
		if err := h.store.SetSessionDeleted(id, true, time.Now()); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
			return
		}
	}
	h.broadcastSessions()
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// handleEmptyTrash permanently deletes every trashed session immediately.
func (h *Hub) handleEmptyTrash(w http.ResponseWriter, r *http.Request) {
	n, err := h.store.PurgeAllDeleted()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	if n > 0 {
		h.broadcastSessions()
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "purged": n})
}

// handleUpdateSession patches mutable session fields: {archived} hides/restores
// from the active tree, {deleted} trashes/restores from Trash.
func (h *Hub) handleUpdateSession(w http.ResponseWriter, r *http.Request, id string) {
	var body struct {
		Archived *bool `json:"archived"`
		Deleted  *bool `json:"deleted"`
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
		// Archiving hides AND frees the machine: end the live process so a stale
		// session stops eating host resources. (A claude session can be resumed
		// later; restoring just un-hides the row.)
		if *body.Archived {
			h.endHostProcess(rec.AgentID, id)
			rec.Status = proto.SessionExited
		}
		if err := h.store.SetSessionArchived(id, *body.Archived, time.Now()); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
			return
		}
		rec.Archived = *body.Archived
	}
	if body.Deleted != nil {
		if *body.Deleted {
			h.endHostProcess(rec.AgentID, id)
			rec.Status = proto.SessionExited
		}
		if err := h.store.SetSessionDeleted(id, *body.Deleted, time.Now()); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
			return
		}
		if *body.Deleted {
			rec.DeletedAt = time.Now()
		} else {
			rec.DeletedAt = time.Time{}
		}
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

	// D32 (amends D20): a session is pinned to the device it was created on — that
	// is THE device for its whole life. Resume goes back to the original host and
	// never re-places onto a different machine. We still run the scorer (purely to
	// confirm that host is currently eligible and to hand the UI a breakdown), but
	// the chosen host is forced to rec.AgentID. If that box is offline/ineligible,
	// resume fails with a clear reason instead of silently migrating the work.
	req := PlacementRequest{
		Kind:        proto.SessionClaude,
		ProjectPath: rec.ProjectPath,
		UserAgentID: body.UserAgentID,
		PinAgentID:  rec.AgentID,
	}
	placement := ScorePlacement(req, h.fleet(), time.Now())
	if placement.Chosen != rec.AgentID {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error":     "can't resume on its device (" + rec.AgentID + "): " + deviceExcludeReason(placement, rec.AgentID) + " — wake it in Fleet",
			"placement": placement,
		})
		return
	}

	resumeID := rec.ClaudeSessionID
	if resumeID == "" {
		resumeID = rec.ID
	}
	out, err := h.createOnAgent(proto.SessionClaude, rec.Scope, rec.ProjectPath, rec.Title, rec.AgentID, resumeID, "", "", rec.NotifyOnIdle, rec.Model, false)
	if err != nil {
		writeJSON(w, statusForRoundTrip(err), map[string]any{"error": err.Error(), "placement": placement})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"session": toSessionView(out), "placement": placement})
}

// handlePlacement answers POST /api/placement with a placement preview (no side
// effects) so the UI can show the ranked machines before committing.
func (h *Hub) handlePlacement(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
		return
	}
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
	// Mirror the create-time default (D32) so the preview pre-selects the Studio
	// for an un-pinned project session.
	scope := strings.TrimSpace(body.Scope)
	if scope == "" {
		scope = "project"
	}
	pin := body.PinAgentID
	if scope == "project" && pin == "" {
		pin = h.defaultPin()
	}
	result := ScorePlacement(PlacementRequest{
		Kind:        kind,
		ProjectPath: body.ProjectPath,
		UserAgentID: body.UserAgentID,
		PinAgentID:  pin,
	}, h.fleet(), time.Now())
	writeJSON(w, http.StatusOK, result)
}

// handleSettings reads (GET) or writes (POST) hub settings. POST body:
// {"key":"primary_agent","value":"<agentId>"}.
func (h *Hub) handleSettings(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		primary, _, _ := h.store.GetSetting("primary_agent")
		writeJSON(w, http.StatusOK, map[string]any{
			"primaryAgent": strings.TrimSpace(primary),
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
		if !isSettableKey(body.Key) {
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

// isSettableKey whitelists the keys writable through the public settings endpoint
// so arbitrary settings can't be written: currently just the primary coding
// machine (D32).
func isSettableKey(key string) bool {
	return key == "primary_agent"
}
