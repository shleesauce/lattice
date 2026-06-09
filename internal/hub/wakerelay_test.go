package hub

import "testing"

// lanAgent builds a test agent with LAN CIDRs + MACs for relay-selection tests.
func lanAgent(id string, online, local bool, macs, lanIPs []string) Agent {
	return Agent{ID: id, Online: online, Local: local, MACs: macs, LANIPs: lanIPs}
}

// TestSelectRelaySameSubnet: among live agents, the one sharing the target's
// subnet is chosen — never an online agent on a different LAN (its broadcast
// would never reach the sleeper).
func TestSelectRelaySameSubnet(t *testing.T) {
	target := lanAgent("studio", false, false, []string{"aa:bb:cc:dd:ee:ff"}, []string{"192.168.1.50/24"})
	fleet := []Agent{
		target,
		lanAgent("offsite", true, false, nil, []string{"10.0.0.5/24"}),    // different LAN
		lanAgent("laptop", true, false, nil, []string{"192.168.1.23/24"}), // same LAN ✓
	}

	c := selectWakeRelay(wakeTargetForAgent(target), fleet)
	if c.RelayID != "laptop" {
		t.Fatalf("expected laptop relay, got %q (reason=%q)", c.RelayID, c.Reason)
	}
	if !c.OnSubnet {
		t.Fatalf("expected OnSubnet=true")
	}
	if c.MAC != "aa:bb:cc:dd:ee:ff" {
		t.Fatalf("expected target MAC, got %q", c.MAC)
	}
	if c.Subnet != "192.168.1.0/24" {
		t.Fatalf("expected matched subnet 192.168.1.0/24, got %q", c.Subnet)
	}
}

// TestSelectRelayPrefersLocal: when two live agents share the target's subnet,
// the local (hub-host) agent is preferred — always-on and snappier.
func TestSelectRelayPrefersLocal(t *testing.T) {
	target := lanAgent("studio", false, false, []string{"aa:bb:cc:dd:ee:ff"}, []string{"192.168.1.50/24"})
	fleet := []Agent{
		target,
		lanAgent("laptop", true, false, nil, []string{"192.168.1.23/24"}),
		lanAgent("hubhost", true, true, nil, []string{"192.168.1.2/24"}), // local
	}

	c := selectWakeRelay(wakeTargetForAgent(target), fleet)
	if c.RelayID != "hubhost" {
		t.Fatalf("expected local hubhost relay, got %q", c.RelayID)
	}
}

// TestSelectRelayNoSubnetReachable: target's subnet is known but no LIVE agent is
// on it ⇒ explicit "no relay reachable on that subnet", never a wrong-LAN relay.
func TestSelectRelayNoSubnetReachable(t *testing.T) {
	target := lanAgent("studio", false, false, []string{"aa:bb:cc:dd:ee:ff"}, []string{"192.168.1.50/24"})
	fleet := []Agent{
		target,
		lanAgent("offsite", true, false, nil, []string{"10.0.0.5/24"}), // wrong LAN
	}

	c := selectWakeRelay(wakeTargetForAgent(target), fleet)
	if c.RelayID != "" {
		t.Fatalf("expected no relay, got %q", c.RelayID)
	}
	if c.Reason != "no relay reachable on that subnet" {
		t.Fatalf("unexpected reason: %q", c.Reason)
	}
}

// TestSelectRelayUnknownSubnetFallback: an older target that never reported
// LANIPs can't be subnet-matched, so we fall back to any live agent (legacy
// best-effort) — but mark OnSubnet=false so the UI/caller knows it's a guess.
func TestSelectRelayUnknownSubnetFallback(t *testing.T) {
	target := lanAgent("studio", false, false, []string{"aa:bb:cc:dd:ee:ff"}, nil) // no LANIPs
	fleet := []Agent{
		target,
		lanAgent("offsite", true, false, nil, []string{"10.0.0.5/24"}),
	}

	c := selectWakeRelay(wakeTargetForAgent(target), fleet)
	if c.RelayID != "offsite" {
		t.Fatalf("expected fallback to offsite, got %q (reason=%q)", c.RelayID, c.Reason)
	}
	if c.OnSubnet {
		t.Fatalf("fallback relay must report OnSubnet=false")
	}
}

// TestSelectRelayUnknownSubnetPrefersLocal: with no subnet to match, the local
// agent is still preferred for the legacy fallback.
func TestSelectRelayUnknownSubnetPrefersLocal(t *testing.T) {
	target := lanAgent("studio", false, false, []string{"aa:bb:cc:dd:ee:ff"}, nil)
	fleet := []Agent{
		target,
		lanAgent("offsite", true, false, nil, []string{"10.0.0.5/24"}),
		lanAgent("hubhost", true, true, nil, []string{"172.16.0.2/24"}),
	}

	c := selectWakeRelay(wakeTargetForAgent(target), fleet)
	if c.RelayID != "hubhost" {
		t.Fatalf("expected local hubhost fallback, got %q", c.RelayID)
	}
}

// TestSelectRelayNeverPicksTarget: a (somehow online) target is never its own
// relay — you can't broadcast a wake from the box you're waking.
func TestSelectRelayNeverPicksTarget(t *testing.T) {
	target := lanAgent("studio", true, false, []string{"aa:bb:cc:dd:ee:ff"}, []string{"192.168.1.50/24"})
	fleet := []Agent{target} // only the target is "live"

	c := selectWakeRelay(wakeTargetForAgent(target), fleet)
	if c.RelayID != "" {
		t.Fatalf("target must not be its own relay, got %q", c.RelayID)
	}
}

// TestSelectRelayNoLiveAgents: nothing online ⇒ a distinct reason (can't even try).
func TestSelectRelayNoLiveAgents(t *testing.T) {
	target := lanAgent("studio", false, false, []string{"aa:bb:cc:dd:ee:ff"}, []string{"192.168.1.50/24"})
	fleet := []Agent{
		target,
		lanAgent("laptop", false, false, nil, []string{"192.168.1.23/24"}), // offline
	}

	c := selectWakeRelay(wakeTargetForAgent(target), fleet)
	if c.RelayID != "" {
		t.Fatalf("expected no relay, got %q", c.RelayID)
	}
	if c.Reason != "no live agent available to send the wake packet" {
		t.Fatalf("unexpected reason: %q", c.Reason)
	}
}

// TestSelectRelayMultiSubnetTarget: a target on TWO subnets (wifi + ethernet) is
// reachable from a relay sharing EITHER.
func TestSelectRelayMultiSubnetTarget(t *testing.T) {
	target := lanAgent("studio", false, false, []string{"aa:bb:cc:dd:ee:ff"},
		[]string{"192.168.1.50/24", "10.10.0.50/24"})
	fleet := []Agent{
		target,
		lanAgent("laptop", true, false, nil, []string{"10.10.0.9/24"}), // shares the 2nd subnet
	}

	c := selectWakeRelay(wakeTargetForAgent(target), fleet)
	if c.RelayID != "laptop" || !c.OnSubnet {
		t.Fatalf("expected laptop on-subnet relay, got %q onSubnet=%v", c.RelayID, c.OnSubnet)
	}
}

// TestAgentSharesSubnetReverse: matching works when the relay's mask is the one
// that contains the target's IP (asymmetric masks).
func TestAgentSharesSubnetReverse(t *testing.T) {
	// Relay advertises a /16 that contains the target's /24 host IP.
	relay := lanAgent("laptop", true, false, nil, []string{"192.168.0.9/16"})
	target := parseCIDRs([]string{"192.168.1.50/24"})
	if sub, ok := agentSharesSubnet(relay, target); !ok {
		t.Fatalf("expected reverse-direction match, got ok=false")
	} else if sub == "" {
		t.Fatalf("expected a non-empty matched subnet")
	}
}

// TestParseCIDRsDropsGarbage: malformed CIDRs are dropped, valid ones kept.
func TestParseCIDRsDropsGarbage(t *testing.T) {
	got := parseCIDRs([]string{"192.168.1.1/24", "not-a-cidr", "", "10.0.0.1/8"})
	if len(got) != 2 {
		t.Fatalf("expected 2 valid CIDRs, got %d", len(got))
	}
}
