package hub

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

// When the requested agent binary isn't in the local dist dir (the common case for
// a get.sh-installed hub), /dl/ must redirect to the public release so an enrolling
// machine can still download the agent — the bug that otherwise blocks every join.
func TestDownloadBinaryRedirectsWhenMissing(t *testing.T) {
	h := &Hub{distDir: t.TempDir()} // empty
	rec := httptest.NewRecorder()
	h.handleDownloadBinary(rec, httptest.NewRequest(http.MethodGet, "/dl/lattice-linux-amd64", nil))

	if rec.Code != http.StatusFound {
		t.Fatalf("status=%d want 302", rec.Code)
	}
	if loc := rec.Header().Get("Location"); loc != agentDownloadBaseDefault+"/lattice-linux-amd64" {
		t.Errorf("Location=%q want %q", loc, agentDownloadBaseDefault+"/lattice-linux-amd64")
	}
}

// A dev/fleet hub with a populated dist dir still serves the binary locally.
func TestDownloadBinaryServesLocalWhenPresent(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "lattice-linux-amd64"), []byte("BINARY"), 0o755); err != nil {
		t.Fatal(err)
	}
	h := &Hub{distDir: dir}
	rec := httptest.NewRecorder()
	h.handleDownloadBinary(rec, httptest.NewRequest(http.MethodGet, "/dl/lattice-linux-amd64", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d want 200", rec.Code)
	}
	if rec.Body.String() != "BINARY" {
		t.Errorf("served body=%q want BINARY", rec.Body.String())
	}
}

// An invalid binary name is rejected before any redirect/serve — no open redirect,
// no path traversal.
func TestDownloadBinaryRejectsBadName(t *testing.T) {
	h := &Hub{distDir: t.TempDir()}
	rec := httptest.NewRecorder()
	h.handleDownloadBinary(rec, httptest.NewRequest(http.MethodGet, "/dl/notabinary", nil))
	if rec.Code != http.StatusNotFound {
		t.Errorf("status=%d want 404 for bad name", rec.Code)
	}
}
