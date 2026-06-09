package hub

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

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
	h.handleUpdate(rec, httptest.NewRequest(http.MethodPost, "/api/update", nil))
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

// An OLDER latest release than the running build is also refused (no downgrade).
func TestHandleUpdateRefusesDowngrade(t *testing.T) {
	h := testHub(t)
	h.version = "v2.0.0"
	primeReleases(h, "v1.5.0")

	rec := httptest.NewRecorder()
	h.handleUpdate(rec, httptest.NewRequest(http.MethodPost, "/api/update", nil))
	if rec.Code != http.StatusConflict {
		t.Fatalf("downgrade attempt: status=%d want 409", rec.Code)
	}
}
