package hub

import (
	"testing"
	"time"

	"github.com/shleesauce/lattice/internal/proto"
)

// agent builds a test agent. claude=true means a fully working claude (installed
// AND authable) — the common case; the installed-but-not-authable case (F14) is
// exercised directly in TestPlacementClaudeNotAuthable.
func agent(id string, online bool, mem, load float64, cores int, claude bool) Agent {
	return Agent{
		ID: id, Online: online, MemUsedPct: mem, LoadAvg1: load, CPUCount: cores,
		Capabilities: proto.Capabilities{ClaudeInstalled: claude, ClaudeAuthable: claude},
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
		agent("laptop", true, 10, 0.1, 8, false), // no claude
		agent("server", true, 50, 0.5, 8, true),
	}
	r := ScorePlacement(PlacementRequest{Kind: proto.SessionClaude}, agents, time.Now())

	laptop, _ := candidate(r, "laptop")
	if laptop.Eligible {
		t.Fatalf("laptop should be ineligible for claude")
	}
	if laptop.Excluded != "claude not installed" {
		t.Fatalf("expected exclusion reason, got %q", laptop.Excluded)
	}
	if r.Chosen != "server" {
		t.Fatalf("expected server chosen, got %q", r.Chosen)
	}
}

// TestPlacementClaudeNotAuthable: an agent with claude installed but that can't
// sign in (F14 — e.g. a hub host under pm2) is hard-excluded from claude with a
// reason and never chosen, while a fully-authable peer wins. A terminal session on
// the same agent stays eligible (it doesn't auth claude).
func TestPlacementClaudeNotAuthable(t *testing.T) {
	hubhost := Agent{
		ID: "hubhost", Online: true, MemUsedPct: 20, LoadAvg1: 0.2, CPUCount: 8,
		Capabilities: proto.Capabilities{ClaudeInstalled: true, ClaudeAuthable: false},
	}
	desktop := agent("desktop", true, 50, 0.5, 8, true) // installed + authable
	agents := []Agent{hubhost, desktop}

	r := ScorePlacement(PlacementRequest{Kind: proto.SessionClaude, PinAgentID: "hubhost"}, agents, time.Now())
	mo, _ := candidate(r, "hubhost")
	if mo.Eligible {
		t.Fatalf("hubhost should be ineligible for claude (not authable)")
	}
	if mo.Excluded != "can't sign in to claude here (needs a desktop login session)" {
		t.Fatalf("expected not-authable reason, got %q", mo.Excluded)
	}
	if r.Chosen != "desktop" {
		t.Fatalf("an un-authable pin must fall back to desktop, got %q", r.Chosen)
	}

	// The same box is fine for a terminal session.
	rt := ScorePlacement(PlacementRequest{Kind: proto.SessionTerminal, PinAgentID: "hubhost"}, agents, time.Now())
	if rt.Chosen != "hubhost" {
		t.Fatalf("terminal should still place on hubhost, got %q", rt.Chosen)
	}
}

// A terminal session ignores the claude capability.
func TestPlacementTerminalIgnoresClaude(t *testing.T) {
	agents := []Agent{agent("laptop", true, 10, 0.1, 8, false)}
	r := ScorePlacement(PlacementRequest{Kind: proto.SessionTerminal}, agents, time.Now())
	if r.Chosen != "laptop" {
		t.Fatalf("terminal should place on laptop, got %q", r.Chosen)
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
		agent("laptop", true, 5, 0.0, 16, false), // pinned but no claude
		agent("server", true, 50, 0.5, 8, true),
	}
	r := ScorePlacement(PlacementRequest{Kind: proto.SessionClaude, PinAgentID: "laptop"}, agents, time.Now())
	if r.Chosen != "server" {
		t.Fatalf("ineligible pin should be ignored, got %q", r.Chosen)
	}
}
