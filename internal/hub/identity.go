package hub

import (
	"log"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/shleesauce/lattice/internal/proto"
)

// Agent identity + duel detection (v0.2.0). This is the centerpiece that retires
// the reconnect-storm class instead of fixing it cause-by-cause.
//
// Before: an agent's id was hostname+os, derived fresh on every register. Two
// machines with the same hostname collided on ONE id and cross-wired sessions;
// and there was no way to tell a benign network reconnect from TWO rival
// processes fighting over one id (each "superseded by reconnect"-evicting the
// other forever).
//
// After: each agent carries a PERSISTENT id (minted once, stored at
// ~/.lattice/agent-id) and a per-PROCESS instance nonce. The hub keys its
// registry on the persistent id (hostname+os are display-only) and uses the
// instance nonce to detect a duel: two live connections for one id with different
// instances are two processes, which is alarmed + resolved deterministically.

// banishTTL is how long a duel-loser's instance is refused re-registration under
// the contested id. It only needs to outlast the loser's reconnect backoff so the
// loser process gives up (and, if unmanaged, exits) rather than re-entering the
// duel; a managed service that restarts gets a FRESH instance nonce and is never
// banished, so it can re-win cleanly. Kept short so a transient misfire self-heals
// fast.
const banishTTL = 60 * time.Second

// resolveAgentID picks the registry key for a register frame (v0.2.0):
//
//   - A non-empty AgentUUID (a v0.2.0 agent that has a persisted id) is trusted
//     and used verbatim — that IS the machine's stable identity.
//   - An empty AgentUUID but a non-empty InstanceID is a v0.2.0 agent on its very
//     first run (it can and WILL persist whatever id it's assigned): reuse the
//     legacy hostname+os id when a record already exists under it (an already-
//     enrolled machine, so its sessions/labels stay attached — the live-fleet
//     migration guarantee), otherwise mint a fresh random UUID. Because a minted id
//     is stored under the UUID (never under hostname+os), a SECOND new box with the
//     same hostname does NOT find a legacy record and gets its OWN distinct UUID —
//     collisions are structurally gone.
//   - An empty AgentUUID AND empty InstanceID is a pre-v0.2.0 agent that CANNOT
//     persist a hub-minted id. Minting one would orphan its sessions and re-mint a
//     brand-new id on every restart, so it is pinned to the stable legacy
//     hostname+os id regardless of record existence — this is the documented
//     "mixed-version fleet degrades to legacy" guarantee.
//
// exists reports whether a persisted agent record already has that id.
func resolveAgentID(reg proto.RegisterPayload, exists func(string) bool) string {
	if id := strings.TrimSpace(reg.AgentUUID); id != "" {
		return id
	}
	legacy := agentID(reg.Hostname, reg.OS)
	// A peer that sends no InstanceID is pre-v0.2.0 and can't persist an assigned id;
	// never mint a UUID it would forget on restart — pin it to the legacy id.
	if strings.TrimSpace(reg.InstanceID) == "" {
		return legacy
	}
	if exists(legacy) {
		return legacy
	}
	return newAgentUUID()
}

// newAgentUUID mints a fresh persistent agent id.
func newAgentUUID() string {
	return uuid.NewString()
}

// agentRecordExists reports whether a persisted agent row with this id exists,
// treating a store error conservatively as "exists" so a transient DB blip can
// NEVER cause the resolver to mint a brand-new id for an already-enrolled machine
// (which would orphan its sessions). The worst case of this bias is that a genuine
// new box momentarily reuses a legacy id on a DB error — recoverable and rare.
func (h *Hub) agentRecordExists(id string) bool {
	ok, err := h.store.AgentExists(id)
	if err != nil {
		log.Printf("identity: agent-exists lookup failed for %q: %v (assuming exists)", id, err)
		return true
	}
	return ok
}

// agentDisplayName returns a human-friendly name for an agent id, for use in
// operator-facing pushes/logs. It prefers the live connection's hostname (or its
// operator label), then a persisted record's name, and finally falls back to the
// legacy prettyAgentName (which strips the -os suffix). This keeps notifications
// readable now that an id may be an opaque UUID rather than "studio-darwin".
func (h *Hub) agentDisplayName(id string) string {
	if ac, ok := h.registry.getAgent(id); ok {
		ac.mu.Lock()
		name := ac.name
		host := ac.hostname
		ac.mu.Unlock()
		if name != "" {
			return name
		}
		if host != "" {
			return host
		}
	}
	if labels, err := h.store.AgentLabels(); err == nil {
		if l := labels[id]; l != "" {
			return l
		}
	}
	return prettyAgentName(id)
}

// duelGuard tracks duel-loser instances that are temporarily refused
// re-registration under a contested agent id. Keyed by "agentID\x00instanceID"
// with an expiry. Concurrency-safe: register() runs one goroutine per inbound
// connection.
type duelGuard struct {
	mu       sync.Mutex
	banished map[string]time.Time // key → expiry
}

func newDuelGuard() *duelGuard {
	return &duelGuard{banished: make(map[string]time.Time)}
}

func duelKey(agentID, instanceID string) string {
	return agentID + "\x00" + instanceID
}

// banish marks an instance as not allowed to (re)register under agentID until
// banishTTL elapses. An empty instanceID is ignored (a pre-v0.2.0 peer can't be
// distinguished, so it's never banished). It also opportunistically reaps expired
// entries so the map can't grow unbounded across many duels.
func (g *duelGuard) banish(agentID, instanceID string, now time.Time) {
	if instanceID == "" {
		return
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	for k, exp := range g.banished {
		if now.After(exp) {
			delete(g.banished, k)
		}
	}
	g.banished[duelKey(agentID, instanceID)] = now.Add(banishTTL)
}

// isBanished reports whether (agentID, instanceID) is currently banished, and
// opportunistically drops the entry if it has expired.
func (g *duelGuard) isBanished(agentID, instanceID string, now time.Time) bool {
	if instanceID == "" {
		return false
	}
	key := duelKey(agentID, instanceID)
	g.mu.Lock()
	defer g.mu.Unlock()
	exp, ok := g.banished[key]
	if !ok {
		return false
	}
	if now.After(exp) {
		delete(g.banished, key)
		return false
	}
	return true
}
