package hub

import (
	"sort"
	"time"

	"github.com/shleesauce/lattice/internal/proto"
)

// Placement weights (D19). Free-RAM dominates so a session lands where it has
// headroom; inverse load + cores break ties; the locality boost nudges toward
// the machine the user is sitting at (snappier + more directly controllable).
const (
	wRAM   = 1.0
	wLoad  = 0.6
	wCores = 0.4
	wLocal = 0.5
)

// coresCap caps the cores term so a 64-core box doesn't dominate purely on count.
const coresCap = 16

// PlacementRequest describes a session we want to place.
type PlacementRequest struct {
	Kind        proto.SessionKind `json:"kind"`
	ProjectPath string            `json:"projectPath"`
	UserAgentID string            `json:"userAgentId"` // the machine the user is on (locality boost)
	PinAgentID  string            `json:"pinAgentId"`  // manual override (wins if eligible)
}

// PlacementCandidate is one agent's score breakdown.
type PlacementCandidate struct {
	AgentID  string             `json:"agentId"`
	Score    float64            `json:"score"`
	Eligible bool               `json:"eligible"`
	Reasons  map[string]float64 `json:"reasons"`
	Excluded string             `json:"excluded,omitempty"`
}

// PlacementResult is the chosen agent plus the full ranked breakdown for the UI.
type PlacementResult struct {
	Chosen     string               `json:"chosen"`
	Candidates []PlacementCandidate `json:"candidates"`
}

// ScorePlacement is the pure placement scorer (D19). It hard-filters offline
// agents (and, for claude, agents without the claude binary OR that can't sign in
// to claude here — F14), scores the rest by
// free RAM + inverse load + cores + a locality boost, honours a manual pin when
// eligible, and returns the full breakdown sorted by score desc (stable).
func ScorePlacement(req PlacementRequest, agents []Agent, now time.Time) PlacementResult {
	cands := make([]PlacementCandidate, 0, len(agents))

	for _, a := range agents {
		c := PlacementCandidate{AgentID: a.ID, Reasons: map[string]float64{}}

		switch {
		case !a.Online:
			c.Excluded = "offline"
		case req.Kind == proto.SessionClaude && !a.Capabilities.ClaudeInstalled:
			c.Excluded = "claude not installed"
		case req.Kind == proto.SessionClaude && !a.Capabilities.ClaudeAuthable:
			// Installed but can't sign in here (F14): a background-service agent
			// (pm2/nohup, e.g. the hub host) has no GUI login keychain for claude's
			// OAuth, so a session placed here would be a dead blank tab. Terminal /
			// editor sessions are unaffected — they don't auth claude.
			c.Excluded = "can't sign in to claude here (needs a desktop login session)"
		case req.Kind == proto.SessionEditor && !a.Capabilities.CodeServerInstalled:
			c.Excluded = "code-server not installed"
		default:
			c.Eligible = true
		}

		if c.Eligible {
			ram := wRAM * (1 - a.MemUsedPct/100)
			load := wLoad * (1 / (1 + a.LoadAvg1))
			cores := wCores * (float64(minInt(a.CPUCount, coresCap)) / float64(coresCap))
			c.Reasons["ram"] = ram
			c.Reasons["load"] = load
			c.Reasons["cores"] = cores
			c.Score = ram + load + cores
			if req.UserAgentID != "" && a.ID == req.UserAgentID {
				c.Reasons["local"] = wLocal
				c.Score += wLocal
			}
		}

		cands = append(cands, c)
	}

	// Sort eligible-first, then by score desc; stable so equal scores keep a
	// deterministic order across previews.
	sort.SliceStable(cands, func(i, j int) bool {
		if cands[i].Eligible != cands[j].Eligible {
			return cands[i].Eligible
		}
		return cands[i].Score > cands[j].Score
	})

	chosen := ""
	if len(cands) > 0 && cands[0].Eligible {
		chosen = cands[0].AgentID
	}

	// A manual pin wins whenever it is eligible (still returning the full
	// breakdown so the UI can show what would have been chosen).
	if req.PinAgentID != "" {
		for _, c := range cands {
			if c.AgentID == req.PinAgentID && c.Eligible {
				chosen = req.PinAgentID
				break
			}
		}
	}

	return PlacementResult{Chosen: chosen, Candidates: cands}
}

// minInt returns the smaller of two ints.
func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
