package hub

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/shleesauce/lattice/internal/proto"
)

// updateResultEnv wraps an UpdateResultPayload in an envelope the way an agent's
// reply arrives, so classifyAgentUpdate can decode it.
func updateResultEnv(t *testing.T, p proto.UpdateResultPayload) proto.Envelope {
	t.Helper()
	raw, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	return proto.Envelope{Type: proto.TypeUpdateResult, Payload: raw}
}

// classifyAgentUpdate is the heart of the v0.1.6 cascade hardening: a timeout or a
// dropped agent must be PENDING (non-fatal), only an explicit agent error is FAILED.
func TestClassifyAgentUpdate(t *testing.T) {
	// Round-trip timeout: the agent most likely swapped + restarted but couldn't ack
	// in time → pending, NOT failed, NOT ok.
	if oc := classifyAgentUpdate("a1", "studio", proto.Envelope{}, errRoundTripTimeout); oc.Status != updateStatusPending || oc.OK || oc.Error != "" {
		t.Fatalf("timeout: got %+v, want status=pending ok=false no-error", oc)
	}
	// Agent offline / send error mid-cascade → pending (skipped this round), not failed.
	if oc := classifyAgentUpdate("a2", "mbp", proto.Envelope{}, errors.New("agent offline")); oc.Status != updateStatusPending || oc.OK {
		t.Fatalf("offline: got %+v, want status=pending", oc)
	}
	// Explicit agent error (fail-closed verify aborted → still on old binary) → failed.
	if oc := classifyAgentUpdate("a3", "emu", updateResultEnv(t, proto.UpdateResultPayload{OK: false, Error: "checksum mismatch"}), nil); oc.Status != updateStatusFailed || oc.OK || oc.Error != "checksum mismatch" {
		t.Fatalf("error: got %+v, want status=failed error=checksum mismatch", oc)
	}
	// Clean ack → updated, ok, label carried through.
	if oc := classifyAgentUpdate("a4", "pc", updateResultEnv(t, proto.UpdateResultPayload{OK: true, Restarted: "sh.lattice.agent"}), nil); oc.Status != updateStatusUpdated || !oc.OK || oc.Restarted != "sh.lattice.agent" {
		t.Fatalf("ok: got %+v, want status=updated ok=true restarted=sh.lattice.agent", oc)
	}
}

// primeReleases injects a non-expired release cache so handleUpdate's
// fetchReleases never touches the network in tests.
func primeReleases(h *Hub, latest string) {
	h.releases = &releaseCache{
		releases:  []releaseInfo{{Version: latest, Name: latest}},
		fetchedAt: time.Now(),
	}
}

func TestHandleUpdateRejectsNonPOST(t *testing.T) {
	h := testHub(t)
	primeReleases(h, "v9.9.9")
	rec := httptest.NewRecorder()
	h.handleUpdate(rec, httptest.NewRequest(http.MethodGet, "/api/update", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("GET /api/update: status=%d want 405", rec.Code)
	}
}

// When the latest release is NOT newer than the running build, the update must be
// refused with 409 — never swap the hub onto the same (or older) build.
func TestHandleUpdateNoUpdateAvailable(t *testing.T) {
	h := testHub(t)
	h.version = "v1.0.0"
	primeReleases(h, "v1.0.0") // same version → nothing to do

	rec := httptest.NewRecorder()
	h.handleUpdate(rec, privileged(httptest.NewRequest(http.MethodPost, "/api/update", nil)))
	if rec.Code != http.StatusConflict {
		t.Fatalf("same-version update: status=%d want 409", rec.Code)
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body["error"] != "no update available" {
		t.Fatalf("expected 'no update available', got %v", body["error"])
	}
}

// An overlapping cascade is rejected (409) so a double-click / two tabs can't
// double-restart the fleet mid-update (v0.1.8 single-flight guard).
func TestHandleUpdateRejectsOverlappingCascade(t *testing.T) {
	h := testHub(t)
	primeReleases(h, "v9.9.9")
	h.updating.Store(true) // simulate a cascade already in flight

	rec := httptest.NewRecorder()
	h.handleUpdate(rec, privileged(httptest.NewRequest(http.MethodPost, "/api/update", nil)))
	if rec.Code != http.StatusConflict {
		t.Fatalf("overlapping update: status=%d want 409", rec.Code)
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body["error"] != "an update is already in progress" {
		t.Fatalf("expected in-progress error, got %v", body["error"])
	}
}

// An OLDER latest release than the running build is also refused (no downgrade).
func TestHandleUpdateRefusesDowngrade(t *testing.T) {
	h := testHub(t)
	h.version = "v2.0.0"
	primeReleases(h, "v1.5.0")

	rec := httptest.NewRecorder()
	h.handleUpdate(rec, privileged(httptest.NewRequest(http.MethodPost, "/api/update", nil)))
	if rec.Code != http.StatusConflict {
		t.Fatalf("downgrade attempt: status=%d want 409", rec.Code)
	}
}

// hubRestartPlan is the v0.2.3 fix for the second mini-ops ops bug: the hub swapped
// its binary, reported a launchd `restartHint` that does not exist on a PM2 host,
// and kept serving the OLD code. The matrix that matters is (service label × PM2).
func TestHubRestartPlan(t *testing.T) {
	for _, tc := range []struct {
		name            string
		label           string
		underPM2        bool
		restartRequired bool
		viaPM2          bool
	}{
		// The mini-ops hub: PM2, no launchd label. Nothing for the operator to do —
		// we exit and PM2's autorestart brings us back on the new binary.
		{"pm2 only", "", true, false, true},
		// PM2 wins over a stale launchd label from an earlier install: kickstarting
		// that label would restart a DIFFERENT process and leave this one on the old
		// build.
		{"pm2 beats a stale launchd label", "sh.lattice.hub", true, false, true},
		// A normally-installed hub: launchd restarts it, no manual step, not PM2.
		{"launchd", "sh.lattice.hub", false, false, false},
		// Bare foreground process / container entrypoint: nobody will restart it, so
		// say so instead of showing a green "done" over stale code.
		{"nothing manages it", "", false, true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			restartRequired, viaPM2 := hubRestartPlan(tc.label, tc.underPM2)
			if restartRequired != tc.restartRequired || viaPM2 != tc.viaPM2 {
				t.Errorf("hubRestartPlan(%q, %v) = (restartRequired=%v, viaPM2=%v), want (%v, %v)",
					tc.label, tc.underPM2, restartRequired, viaPM2, tc.restartRequired, tc.viaPM2)
			}
			// The two outcomes are mutually exclusive: "you must restart it" and
			// "PM2 is restarting it" can never both be true.
			if restartRequired && viaPM2 {
				t.Error("reported both restartRequired and viaPM2")
			}
		})
	}
}
