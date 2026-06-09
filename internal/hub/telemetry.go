package hub

import (
	"encoding/json"
	"net/http"
	"os"
	"strings"

	"github.com/shleesauce/lattice/internal/proto"
	"github.com/shleesauce/lattice/internal/transcript"
)

// Rich session telemetry (C, v0.1.5). Claude Code hook stdin does NOT carry model /
// context% / $cost, so we DERIVE them hub-side from the session's synced transcript
// (internal/transcript already parses usage{input,output,cache_*} + model per
// assistant turn). GET /api/sessions/{id}/telemetry returns the compact summary the
// dashboard renders on the ai-beacon-style session card — without shipping the full
// blocks the transcript view needs.

// modelContextWindow maps a model id (or its family) to its usable context window in
// tokens, so the card can show a context% gauge. Defaults to 200K (the standard
// window) when unknown; the [1m] 1M-context variants get 1,000,000.
func modelContextWindow(model string) int {
	m := strings.ToLower(strings.TrimSpace(model))
	if m == "" {
		return 200_000
	}
	if strings.Contains(m, "[1m]") || strings.Contains(m, "-1m") {
		return 1_000_000
	}
	return 200_000
}

// telemetryView is the compact card summary. Tokens are the running conversation
// totals; ContextPct is the last-turn input footprint against the model window;
// CostUSD is an informational estimate (see estimateCostUSD).
type telemetryView struct {
	SessionID    string  `json:"sessionId"`
	Found        bool    `json:"found"`
	Model        string  `json:"model,omitempty"`
	InputTokens  int     `json:"inputTokens"`
	OutputTokens int     `json:"outputTokens"`
	CacheRead    int     `json:"cacheReadTokens"`
	CacheCreate  int     `json:"cacheCreateTokens"`
	MessageCount int     `json:"messageCount"`
	ContextPct   float64 `json:"contextPct"`       // 0..100, current context footprint
	CostUSD      float64 `json:"costUsd"`          // informational estimate
	LastAt       string  `json:"lastAt,omitempty"` // RFC3339 of the last turn
	PRURL        string  `json:"prUrl,omitempty"`  // detected PR for this session (D)
}

// handleSessionTelemetry answers GET /api/sessions/{id}/telemetry. Network-gated
// like the other dashboard REST endpoints. A missing transcript returns
// 200 {found:false} so the card degrades gracefully (no transcript synced yet, or a
// terminal/editor session).
func (h *Hub) handleSessionTelemetry(w http.ResponseWriter, r *http.Request, id string) {
	rec, ok, err := h.store.GetSession(id)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	if !ok {
		http.NotFound(w, r)
		return
	}
	resp := telemetryView{SessionID: id, PRURL: rec.PRURL}
	if proto.SessionKind(rec.Kind) != proto.SessionClaude {
		writeJSON(w, http.StatusOK, resp)
		return
	}

	claudeID := rec.ClaudeSessionID
	if claudeID == "" {
		claudeID = rec.ID
	}

	var meta transcript.Meta
	gotMeta := false

	// Prefer the owning agent (the box that holds the .jsonl) when online.
	if _, online := h.registry.liveAgent(rec.AgentID); online {
		if got, ferr := h.fetchTranscriptFromAgent(rec.AgentID, rec.ID, claudeID); ferr == nil && got.Found && len(got.Meta) > 0 {
			if json.Unmarshal(got.Meta, &meta) == nil {
				gotMeta = true
			}
		}
	}
	// Fallback to hub-local disk (rare hub-hosted session).
	if !gotMeta {
		if home, herr := os.UserHomeDir(); herr == nil {
			if _, m, _, found := transcript.ParseFile(home, claudeID); found {
				meta = m
				gotMeta = true
			}
		}
	}
	if !gotMeta {
		writeJSON(w, http.StatusOK, resp)
		return
	}

	resp.Found = true
	resp.Model = meta.Model
	// Fall back to the launched model id when the transcript hasn't recorded one yet
	// (e.g. brand-new session, first turn not synced).
	if resp.Model == "" {
		resp.Model = rec.Model
	}
	resp.InputTokens = meta.InputTokens
	resp.OutputTokens = meta.OutputTokens
	resp.CacheRead = meta.CacheReadTokens
	resp.CacheCreate = meta.CacheCreationTokens
	resp.MessageCount = meta.MessageCount
	resp.LastAt = meta.LastAt
	resp.ContextPct = contextPct(meta, resp.Model)
	resp.CostUSD = estimateCostUSD(meta)
	writeJSON(w, http.StatusOK, resp)
}

// contextPct estimates how full the model's context window is. The transcript sums
// usage across every assistant call, so the cache-read + last input footprint is a
// reasonable proxy for the live working set. We approximate the current footprint as
// the cache-read tokens (the re-sent prompt prefix) plus output, capped at the
// window — close enough for the at-a-glance card gauge.
func contextPct(m transcript.Meta, model string) float64 {
	win := modelContextWindow(model)
	if win <= 0 {
		return 0
	}
	// cache_read approximates the prompt prefix re-sent each turn (the live context);
	// add the last output as a small headroom term. This intentionally tracks the
	// working set, not the lifetime token sum.
	footprint := m.CacheReadTokens + m.OutputTokens
	if footprint <= 0 {
		// No cache reads yet (very short session): use raw input as the footprint.
		footprint = m.InputTokens
	}
	pct := float64(footprint) / float64(win) * 100
	if pct > 100 {
		pct = 100
	}
	if pct < 0 {
		pct = 0
	}
	return pct
}

// estimateCostUSD is an INFORMATIONAL dollar estimate of the conversation, mirroring
// Claude Code's own cost meter. It uses representative Opus-class blended rates (per
// million tokens); this is a ballpark for the card, NOT billing — Lattice runs on
// the Max subscription (D35), so real usage is subscription-metered, not per-token.
func estimateCostUSD(m transcript.Meta) float64 {
	const (
		inPerM         = 15.0 // input
		outPerM        = 75.0 // output
		cacheReadPerM  = 1.5  // cache read (≈10% of input)
		cacheWritePerM = 18.75
	)
	cost := float64(m.InputTokens)/1e6*inPerM +
		float64(m.OutputTokens)/1e6*outPerM +
		float64(m.CacheReadTokens)/1e6*cacheReadPerM +
		float64(m.CacheCreationTokens)/1e6*cacheWritePerM
	if cost < 0 {
		cost = 0
	}
	return cost
}
