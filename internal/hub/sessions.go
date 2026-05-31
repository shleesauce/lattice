package hub

import (
	"encoding/json"
	"log"
	"strings"
	"time"

	"github.com/dylanstoryyy/lattice/internal/proto"
)

// auditDetailMax caps the verbatim event slice stored per audit row so a huge
// tool_result can't bloat the DB.
const auditDetailMax = 8 << 10 // 8 KiB

// adoptSessions reconciles the agent's live session list into the DB (D18 / F).
// For each descriptor: an existing row is rebound to the agent and marked live;
// a missing row is reconstructed from the descriptor (self-healing across a hub
// restart where the row was never written or was cleared).
func (h *Hub) adoptSessions(agentID string, descs []proto.SessionDescriptor) {
	now := time.Now()
	for _, d := range descs {
		_, exists, err := h.store.GetSession(d.SessionID)
		if err != nil {
			log.Printf("adopt: get session %s failed: %v", d.SessionID, err)
			continue
		}
		if exists {
			if err := h.store.SetSessionAgent(d.SessionID, agentID, d.ClaudeSessionID); err != nil {
				log.Printf("adopt: rebind %s failed: %v", d.SessionID, err)
			}
			if err := h.store.UpdateSessionStatus(d.SessionID, proto.SessionLive, now); err != nil {
				log.Printf("adopt: relive %s failed: %v", d.SessionID, err)
			}
			continue
		}
		created := now
		if t, perr := time.Parse(time.RFC3339, d.StartedAt); perr == nil {
			created = t
		}
		if err := h.store.UpsertSession(SessionRecord{
			ID:              d.SessionID,
			ProjectPath:     d.Cwd,
			Kind:            string(d.Kind),
			AgentID:         agentID,
			ClaudeSessionID: d.ClaudeSessionID,
			Status:          proto.SessionLive,
			CreatedAt:       created,
			LastActiveAt:    now,
		}); err != nil {
			log.Printf("adopt: reconstruct %s failed: %v", d.SessionID, err)
		}
	}
}

// auditClaudeEvent logs tool_use / tool_result / result events for a claude
// session (D21). Other event subtypes (assistant text, usage, system) are not
// audited. detail_json is the verbatim event capped to auditDetailMax.
func (h *Hub) auditClaudeEvent(agentID string, p proto.ClaudeEventPayload) {
	if !isAuditable(p.Subtype) {
		return
	}
	tool := extractToolName(p.Raw)
	detail := string(p.Raw)
	if len(detail) > auditDetailMax {
		detail = detail[:auditDetailMax]
	}
	if err := h.store.InsertAudit(p.SessionID, agentID, p.Subtype, tool, detail, time.Now()); err != nil {
		log.Printf("audit: insert failed session=%s: %v", p.SessionID, err)
	}
}

// isAuditable reports whether a stream-json subtype warrants an audit row. The
// top-level "type" of assistant turns is "assistant"; tool calls/results appear
// inside content blocks, so we also catch the nested-content case in the caller
// via extractToolName. We audit assistant turns that carry tool_use plus the
// terminal "result" event.
func isAuditable(subtype string) bool {
	switch subtype {
	case "assistant", "user", "tool_use", "tool_result", "result":
		return true
	default:
		return false
	}
}

// extractToolName best-effort pulls a tool name out of a stream-json event,
// handling both a top-level tool_use and an assistant message whose content
// array contains a tool_use block.
func extractToolName(raw json.RawMessage) string {
	var top struct {
		Type    string `json:"type"`
		Name    string `json:"name"`
		Message struct {
			Content []struct {
				Type string `json:"type"`
				Name string `json:"name"`
			} `json:"content"`
		} `json:"message"`
	}
	if err := json.Unmarshal(raw, &top); err != nil {
		return ""
	}
	if top.Type == "tool_use" && top.Name != "" {
		return top.Name
	}
	for _, c := range top.Message.Content {
		if c.Type == "tool_use" && c.Name != "" {
			return c.Name
		}
	}
	return ""
}

// sessionView is the JSON shape returned for a session row.
type sessionView struct {
	ID              string `json:"id"`
	ProjectPath     string `json:"projectPath"`
	Kind            string `json:"kind"`
	AgentID         string `json:"agentId"`
	ClaudeSessionID string `json:"claudeSessionId,omitempty"`
	Title           string `json:"title"`
	Status          string `json:"status"`
	Pinned          bool   `json:"pinned"`
	Scope           string `json:"scope"`
	Archived        bool   `json:"archived"`
	CreatedAt       string `json:"createdAt"`
	LastActiveAt    string `json:"lastActiveAt"`
}

// toSessionView renders a SessionRecord for the API.
func toSessionView(r SessionRecord) sessionView {
	return sessionView{
		ID:              r.ID,
		ProjectPath:     r.ProjectPath,
		Kind:            r.Kind,
		AgentID:         r.AgentID,
		ClaudeSessionID: r.ClaudeSessionID,
		Title:           r.Title,
		Status:          r.Status,
		Pinned:          r.Pinned,
		Scope:           r.Scope,
		Archived:        r.Archived,
		CreatedAt:       r.CreatedAt.UTC().Format(time.RFC3339),
		LastActiveAt:    r.LastActiveAt.UTC().Format(time.RFC3339),
	}
}

// forceApproval reports whether approval mode must be forced for an agent: the
// global kill switch OR a per-machine override (D21). When forced, sessions run
// with --permission-mode default (SkipPerms=false).
func (h *Hub) forceApproval(agentID string) bool {
	if v, ok, _ := h.store.GetSetting("force_approval_global"); ok && isTrue(v) {
		return true
	}
	if v, ok, _ := h.store.GetSetting("force_approval:" + agentID); ok && isTrue(v) {
		return true
	}
	return false
}

// isTrue parses a settings flag value.
func isTrue(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}
