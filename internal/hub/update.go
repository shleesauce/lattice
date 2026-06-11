package hub

import (
	"context"
	"errors"
	"log"
	"net/http"
	"time"

	"github.com/shleesauce/lattice/internal/proto"
	"github.com/shleesauce/lattice/internal/update"
)

// updateAgentTimeout bounds a single agent's update round-trip. An agent has to
// download the release asset, verify its SHA256SUMS, and swap the binary before
// it answers, so this is far longer than the 10s file/wake pendingTimeout.
const updateAgentTimeout = 90 * time.Second

// Per-agent update status. "pending" is the key v0.1.6 addition: a timeout or an
// agent that dropped mid-cascade is NOT a failure — the fleet is intact and the
// swapped binary still applies, so it must not show up as a red "failed".
const (
	updateStatusUpdated = "updated" // agent acked OK; new binary live (or applies on its restart)
	updateStatusPending = "pending" // no clean ack (timeout / offline) — non-fatal, applies on next start
	updateStatusFailed  = "failed"  // agent explicitly reported an error; STILL on its old binary
)

// agentUpdateOutcome is one agent's result in the fleet-update summary. Status is
// the tri-state the UI renders; OK is kept (== updated) for back-compat; Detail is
// a short human note for the pending/failed cases.
type agentUpdateOutcome struct {
	AgentID   string `json:"agentId"`
	Name      string `json:"name"`
	Status    string `json:"status"`
	OK        bool   `json:"ok"`
	Restarted string `json:"restarted,omitempty"`
	Detail    string `json:"detail,omitempty"`
	Error     string `json:"error,omitempty"`
}

// classifyAgentUpdate maps a single agent's round-trip result to an outcome. Pure
// (no hub state) so the classification is table-testable. A round-trip timeout or
// an offline agent is PENDING — the agent most likely swapped its binary and just
// couldn't ack before its own restart (or it dropped), and either way the fleet is
// fine and the binary applies on next start. Only an explicit agent-reported error
// (verify/swap aborted → still on the old binary) is a real FAILED.
func classifyAgentUpdate(id, name string, env proto.Envelope, rtErr error) agentUpdateOutcome {
	oc := agentUpdateOutcome{AgentID: id, Name: name}
	if rtErr != nil {
		oc.Status = updateStatusPending
		if errors.Is(rtErr, errRoundTripTimeout) {
			oc.Detail = "no confirmation yet — applies on restart"
		} else {
			oc.Detail = "went offline before confirming — not updated this round"
		}
		return oc
	}
	var res proto.UpdateResultPayload
	if err := proto.As(env, &res); err != nil {
		oc.Status = updateStatusFailed
		oc.Error = "bad agent response"
		return oc
	}
	if res.OK {
		oc.Status = updateStatusUpdated
		oc.OK = true
		oc.Restarted = res.Restarted
		return oc
	}
	oc.Status = updateStatusFailed
	oc.Error = res.Error
	return oc
}

// handleUpdate runs the one-click fleet auto-update (admin-gated, v0.1.5 / H):
//
//  1. Confirm an update is actually available (don't swap onto the same build).
//  2. Self-update the HUB first — pull+verify+swap its own binary. The resolved
//     download base it used is threaded down to every agent so the whole fleet
//     lands on the IDENTICAL build (lockstep, D34) regardless of each box's own
//     $LATTICE_DOWNLOAD_BASE.
//  3. Cascade to every online agent SEQUENTIALLY (lockstep): each agent verifies
//     + swaps + restarts its own service and reports back before the next starts.
//  4. Return the full summary, THEN restart the hub in the background so the HTTP
//     response (and the dashboard's progress view) makes it out before the hub
//     process is replaced and the browser reconnects to the new build.
//
// Verification is fail-closed end-to-end: update.Apply aborts (binary untouched)
// on a missing/bad SHA256SUMS, so a tampered or unreachable release can never be
// installed on the hub or any agent.
func (h *Hub) handleUpdate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Single-flight: reject an overlapping cascade (impatient double-click, two
	// tabs) so agents can't be double-restarted mid-update (v0.1.8). Released when
	// this handler returns — the cascade round-trips run synchronously below, before
	// the response, so the guard is held for the whole fleet pass.
	if !h.updating.CompareAndSwap(false, true) {
		writeJSON(w, http.StatusConflict, map[string]any{
			"error": "an update is already in progress",
		})
		return
	}
	defer h.updating.Store(false)

	// Gate on a real, newer release. fetchReleases is cached + stale-on-error, so
	// this is cheap and never blocks on a GitHub blip.
	releases, _ := h.fetchReleases(r.Context())
	ls, ok := latestStable(releases)
	if !ok || !versionNewer(ls.Version, h.version) {
		writeJSON(w, http.StatusConflict, map[string]any{
			"error":   "no update available",
			"current": h.version,
		})
		return
	}
	target := ls.Version
	log.Printf("update: starting fleet update %s → %s", h.version, target)

	// 1) Self-update the hub. Use a detached context so an aborting browser (the
	// operator navigating away mid-update) can't cancel the binary swap partway.
	base, err := update.Apply(context.Background(), update.Options{})
	if err != nil {
		log.Printf("update: hub self-update failed: %v", err)
		writeJSON(w, http.StatusBadGateway, map[string]any{
			"error":   "hub update failed: " + err.Error(),
			"current": h.version,
		})
		return
	}
	log.Printf("update: hub binary swapped to %s (base %s)", target, base)

	// 2) Cascade to agents in lockstep, threading the hub's resolved base so every
	// agent pulls the exact build the hub just installed.
	agents := h.updateAgents(base, target)

	// 3) Respond with the summary, then restart the hub AFTER the response flushes.
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":       true,
		"updating": true,
		"from":     h.version,
		"to":       target,
		"agents":   agents,
	})

	go func() {
		// Let the HTTP response (and the dashboard's progress render) reach the
		// browser before the restart yanks the listener out from under it.
		time.Sleep(750 * time.Millisecond)
		if label := update.Restart(); label != "" {
			log.Printf("update: hub restarting via %s to apply %s", label, target)
		} else {
			log.Printf("update: no installed hub service to restart; new binary (%s) applies on next start", target)
		}
	}()
}

// updateAgents pushes the update to every online agent ONE AT A TIME and collects
// the per-agent outcomes. Sequential (not fan-out) is the lockstep guarantee: a
// failure is visible before the next agent is touched, and the fleet never sits
// with a mix of mid-swap binaries racing the hub's restart.
func (h *Hub) updateAgents(base, target string) []agentUpdateOutcome {
	// Snapshot the live fleet up front so reconnects mid-cascade don't reshuffle
	// what we iterate.
	online := h.registry.snapshot(offlineAfter)
	out := make([]agentUpdateOutcome, 0, len(online))

	for _, a := range online {
		if !a.Online {
			continue
		}
		// An agent can drop between the snapshot and its turn (a laptop sleeps mid-
		// cascade). Re-check liveness right before dispatch so a dead agent is marked
		// pending IMMEDIATELY instead of blocking the whole fleet for updateAgentTimeout.
		if _, ok := h.registry.liveAgent(a.ID); !ok {
			oc := agentUpdateOutcome{
				AgentID: a.ID, Name: a.Name,
				Status: updateStatusPending,
				Detail: "went offline before update — skipped this round",
			}
			log.Printf("update: agent %s offline at its turn; marked pending (skipped)", a.ID)
			out = append(out, oc)
			continue
		}

		reqID := newReqID()
		env, err := h.roundTripT(a.ID, reqID, updateAgentTimeout, proto.TypeUpdate, proto.UpdatePayload{
			ReqID: reqID, Base: base, Version: target,
		})
		oc := classifyAgentUpdate(a.ID, a.Name, env, err)
		switch oc.Status {
		case updateStatusUpdated:
			log.Printf("update: agent %s updated to %s (restarted %q)", a.ID, target, oc.Restarted)
		case updateStatusPending:
			log.Printf("update: agent %s pending: %s", a.ID, oc.Detail)
		default:
			log.Printf("update: agent %s update FAILED: %s", a.ID, oc.Error)
		}
		out = append(out, oc)
	}
	return out
}
