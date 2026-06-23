package hub

import (
	"testing"
	"time"

	"github.com/shleesauce/lattice/internal/proto"
)

// TestResolveAgentID covers the identity-resolution paths that make the live-fleet
// migration safe: trust a persisted UUID, reuse a legacy id for an already-enrolled
// box (no orphaning), mint a fresh UUID for a brand-new v0.2.0 box, and pin a
// pre-v0.2.0 agent (no InstanceID, can't persist) to the legacy id even when no
// record exists yet — minting an id it would forget on restart would orphan it.
func TestResolveAgentID(t *testing.T) {
	t.Run("trusts persisted AgentUUID verbatim", func(t *testing.T) {
		reg := proto.RegisterPayload{AgentUUID: "fixed-uuid-123", Hostname: "studio", OS: "darwin"}
		// exists must NOT be consulted when a UUID is present.
		got := resolveAgentID(reg, func(string) bool {
			t.Fatalf("exists should not be called when AgentUUID is set")
			return false
		})
		if got != "fixed-uuid-123" {
			t.Fatalf("got %q want fixed-uuid-123", got)
		}
	})

	t.Run("reuses legacy id for an already-enrolled box", func(t *testing.T) {
		reg := proto.RegisterPayload{Hostname: "Studio", OS: "darwin"} // no UUID (pre-v0.2.0 / first run)
		legacy := agentID(reg.Hostname, reg.OS)                        // "studio-darwin"
		got := resolveAgentID(reg, func(id string) bool { return id == legacy })
		if got != legacy {
			t.Fatalf("got %q want legacy %q (continuity must hold so sessions don't orphan)", got, legacy)
		}
	})

	t.Run("mints a fresh UUID for a brand-new v0.2.0 box", func(t *testing.T) {
		// A v0.2.0 agent carries an InstanceID even before it has a persisted id, so
		// it can store whatever the hub assigns — minting is safe.
		reg := proto.RegisterPayload{Hostname: "laptop", OS: "darwin", InstanceID: "inst-a"}
		legacy := agentID(reg.Hostname, reg.OS)
		got := resolveAgentID(reg, func(string) bool { return false }) // nothing enrolled yet
		if got == "" || got == legacy {
			t.Fatalf("expected a fresh UUID distinct from legacy %q, got %q", legacy, got)
		}
		// A second new box with the SAME hostname (still no record under the legacy
		// key, because the first was stored under its UUID) must get a DIFFERENT id.
		got2 := resolveAgentID(reg, func(string) bool { return false })
		if got2 == got {
			t.Fatalf("two same-hostname new boxes collided on %q", got)
		}
	})

	t.Run("pins a pre-v0.2.0 agent to the legacy id without minting", func(t *testing.T) {
		// No AgentUUID and no InstanceID ⇒ pre-v0.2.0: it can't persist a minted id,
		// so even with NO existing record it must get the stable legacy id (not a
		// fresh UUID it would forget on restart, orphaning its sessions every time).
		reg := proto.RegisterPayload{Hostname: "oldbox", OS: "linux"}
		legacy := agentID(reg.Hostname, reg.OS)
		got := resolveAgentID(reg, func(string) bool { return false }) // nothing enrolled yet
		if got != legacy {
			t.Fatalf("got %q want legacy %q (a pre-v0.2.0 agent must never be minted a UUID)", got, legacy)
		}
	})
}

// TestDuelGuard covers banish → isBanished → TTL expiry, the mechanism that makes
// a duel-loser process give up instead of re-entering the reconnect storm.
func TestDuelGuard(t *testing.T) {
	g := newDuelGuard()
	now := time.Date(2026, 6, 11, 12, 0, 0, 0, time.UTC)

	if g.isBanished("studio-darwin", "inst-A", now) {
		t.Fatal("nothing banished yet")
	}

	g.banish("studio-darwin", "inst-A", now)
	if !g.isBanished("studio-darwin", "inst-A", now) {
		t.Fatal("inst-A should be banished")
	}
	// A different instance under the same id is fine (the winner must be able to
	// register), as is the same instance under a different id.
	if g.isBanished("studio-darwin", "inst-B", now) {
		t.Fatal("inst-B must not be banished")
	}
	if g.isBanished("other-darwin", "inst-A", now) {
		t.Fatal("inst-A under a different id must not be banished")
	}

	// Expires after banishTTL so a transient misfire self-heals.
	if !g.isBanished("studio-darwin", "inst-A", now.Add(banishTTL-time.Second)) {
		t.Fatal("still banished just before TTL")
	}
	if g.isBanished("studio-darwin", "inst-A", now.Add(banishTTL+time.Second)) {
		t.Fatal("should have expired past TTL")
	}

	// An empty instance is never banished (a pre-v0.2.0 peer can't be adjudicated).
	g.banish("studio-darwin", "", now)
	if g.isBanished("studio-darwin", "", now) {
		t.Fatal("empty instance must never be banished")
	}
}
