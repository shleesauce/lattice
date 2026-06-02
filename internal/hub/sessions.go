package hub

import (
	"log"
	"strings"
	"time"

	"github.com/dylanstoryyy/lattice/internal/proto"
)

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
	DeletedAt       string `json:"deletedAt,omitempty"`
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
		DeletedAt:       deletedAtStr(r.DeletedAt),
		CreatedAt:       r.CreatedAt.UTC().Format(time.RFC3339),
		LastActiveAt:    r.LastActiveAt.UTC().Format(time.RFC3339),
	}
}

// deletedAtStr renders a trash timestamp, or "" when the session isn't trashed.
func deletedAtStr(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}

// defaultPin returns the configured primary coding machine (D32) as a soft pin
// for project sessions created without an explicit device pick. Empty when unset.
// The pin is only honoured by ScorePlacement when that agent is actually eligible
// for the requested kind, so an offline/unsuitable primary transparently falls
// back to the best available host.
func (h *Hub) defaultPin() string {
	v, ok, _ := h.store.GetSetting("primary_agent")
	if !ok {
		return ""
	}
	return strings.TrimSpace(v)
}
