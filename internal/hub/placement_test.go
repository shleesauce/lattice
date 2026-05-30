package hub

import (
	"testing"
	"time"

	"github.com/dylanstoryyy/lattice/internal/proto"
)

func agent(id string, online bool, mem, load float64, cores int, claude bool) Agent {
	return Agent{
		ID: id, Online: online, MemUsedPct: mem, LoadAvg1: load, CPUCount: cores,
		Capabilities: proto.Capabilities{ClaudeInstalled: claude},
	}
}

func candidate(r PlacementResult, id string) (PlacementCandidate, bool) {
	for _, c := range r.Candidates {
		if c.AgentID == id {
			return c, true
		}
	}
	return PlacementCandidate{}, false
}

// TestPlacementCapabilityExclusion: a claude session must hard-exclude an agent
// without claude, with a reason, and never choose it.
func TestPlacementCapabilityExclusion(t *testing.T) {
	agents := []Agent{
		agent("mbp", true, 10, 0.1, 8, false), // no claude
		agent("mini", true, 50, 0.5, 8, true),
	}
	r := ScorePlacement(PlacementRequest{Kind: proto.SessionClaude}, agents, time.Now())

	mbp, _ := candidate(r, "mbp")
	if mbp.Eligible {
		t.Fatalf("mbp should be ineligible for claude")
	}
	if mbp.Excluded != "claude not installed" {
		t.Fatalf("expected exclusion reason, got %q", mbp.Excluded)
	}
	if r.Chosen != "mini" {
		t.Fatalf("expected mini chosen, got %q", r.Chosen)
	}
}

// A terminal session ignores the claude capability.
func TestPlacementTerminalIgnoresClaude(t *testing.T) {
	agents := []Agent{agent("mbp", true, 10, 0.1, 8, false)}
	r := ScorePlacement(PlacementRequest{Kind: proto.SessionTerminal}, agents, time.Now())
	if r.Chosen != "mbp" {
		t.Fatalf("terminal should place on mbp, got %q", r.Chosen)
	}
}

// Offline agents are always excluded.
func TestPlacementOfflineExcluded(t *testing.T) {
	agents := []Agent{
		agent("off", false, 1, 0.0, 16, true),
		agent("on", true, 90, 5.0, 2, true),
	}
	r := ScorePlacement(PlacementRequest{Kind: proto.SessionClaude}, agents, time.Now())
	off, _ := candidate(r, "off")
	if off.Eligible || off.Excluded != "offline" {
		t.Fatalf("offline agent should be excluded: %+v", off)
	}
	if r.Chosen != "on" {
		t.Fatalf("expected on chosen, got %q", r.Chosen)
	}
}

// The locality boost lets the user's machine win a tie it would otherwise lose.
func TestPlacementLocalityBoost(t *testing.T) {
	// Two identical agents; without locality the stable sort keeps input order.
	agents := []Agent{
		agent("remote", true, 50, 0.5, 8, true),
		agent("local", true, 50, 0.5, 8, true),
	}
	r := ScorePlacement(PlacementRequest{Kind: proto.SessionClaude, UserAgentID: "local"}, agents, time.Now())
	if r.Chosen != "local" {
		t.Fatalf("locality boost should pick local, got %q", r.Chosen)
	}
	local, _ := candidate(r, "local")
	if local.Reasons["local"] != wLocal {
		t.Fatalf("expected local reason %v, got %v", wLocal, local.Reasons["local"])
	}
}

// A manual pin wins even when it is not the top score, as long as it's eligible.
func TestPlacementPinOverride(t *testing.T) {
	agents := []Agent{
		agent("best", true, 5, 0.0, 16, true),   // highest headroom
		agent("pinned", true, 80, 4.0, 2, true), // worse, but pinned
	}
	r := ScorePlacement(PlacementRequest{Kind: proto.SessionClaude, PinAgentID: "pinned"}, agents, time.Now())
	if r.Chosen != "pinned" {
		t.Fatalf("pin should win, got %q", r.Chosen)
	}
	// Full breakdown is still returned.
	if _, ok := candidate(r, "best"); !ok {
		t.Fatalf("breakdown should include best")
	}
}

// An ineligible pin is ignored (falls back to the top eligible score).
func TestPlacementPinIneligibleIgnored(t *testing.T) {
	agents := []Agent{
		agent("mbp", true, 5, 0.0, 16, false), // pinned but no claude
		agent("mini", true, 50, 0.5, 8, true),
	}
	r := ScorePlacement(PlacementRequest{Kind: proto.SessionClaude, PinAgentID: "mbp"}, agents, time.Now())
	if r.Chosen != "mini" {
		t.Fatalf("ineligible pin should be ignored, got %q", r.Chosen)
	}
}
