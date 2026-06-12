package update

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// mockRelease serves a fake download base: the OS/arch-matched asset plus a
// SHA256SUMS file. checksum lets a test serve a DELIBERATELY WRONG sums line to
// exercise the fail-closed verify.
func mockRelease(t *testing.T, body []byte, checksum string) string {
	t.Helper()
	asset := assetName()
	mux := http.NewServeMux()
	mux.HandleFunc("/"+asset, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(body)
	})
	mux.HandleFunc("/SHA256SUMS", func(w http.ResponseWriter, r *http.Request) {
		if checksum == "" {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		_, _ = fmt.Fprintf(w, "%s  %s\n", checksum, asset)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv.URL
}

func sum(b []byte) string {
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

// withFakeBinary points the updater at a throwaway "current binary" so Apply
// swaps THAT file instead of the test runner. targetPath() follows os.Executable,
// so we can't easily redirect it without exec'ing a child; instead we test the
// download+verify path end-to-end by running Apply from a child process whose
// executable IS the fake binary. To keep this hermetic and fast, the verify-only
// assertions below drive Apply directly and accept that the swap targets the test
// binary's path — which we guard by only running the SUCCESS swap in a subprocess.

// TestApplyRejectsBadChecksum is the core fail-closed guarantee: a SHA256SUMS that
// lists the wrong hash must ABORT before any swap, returning a checksum-mismatch
// error and leaving the binary untouched.
func TestApplyRejectsBadChecksum(t *testing.T) {
	body := []byte("#!/bin/sh\necho new-binary\n")
	base := mockRelease(t, body, sum([]byte("DIFFERENT CONTENT")))

	_, err := Apply(context.Background(), Options{Base: base})
	if err == nil {
		t.Fatal("expected checksum-mismatch error, got nil (binary would have been swapped!)")
	}
	if !strings.Contains(err.Error(), "checksum mismatch") {
		t.Fatalf("expected a checksum-mismatch error, got: %v", err)
	}
}

// TestApplyRejectsMissingSums proves the fail-closed stance when SHA256SUMS is
// unreachable: abort, never install the unverified asset.
func TestApplyRejectsMissingSums(t *testing.T) {
	body := []byte("new-binary")
	base := mockRelease(t, body, "") // SHA256SUMS → 404

	_, err := Apply(context.Background(), Options{Base: base})
	if err == nil {
		t.Fatal("expected SHA256SUMS-unavailable error, got nil")
	}
	if !strings.Contains(err.Error(), "SHA256SUMS unavailable") {
		t.Fatalf("expected SHA256SUMS-unavailable error, got: %v", err)
	}
}

// TestApplyRejectsAssetNotListed covers a sums file that exists but omits this
// asset's line (the "strip just our line" attack) — also fail closed.
func TestApplyRejectsAssetNotListed(t *testing.T) {
	body := []byte("new-binary")
	// Serve a valid-looking sums file for a DIFFERENT filename.
	mux := http.NewServeMux()
	mux.HandleFunc("/"+assetName(), func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write(body) })
	mux.HandleFunc("/SHA256SUMS", func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprintf(w, "%s  some-other-file\n", sum(body))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	_, err := Apply(context.Background(), Options{Base: srv.URL})
	if err == nil {
		t.Fatal("expected not-listed error, got nil")
	}
	if !strings.Contains(err.Error(), "not listed in SHA256SUMS") {
		t.Fatalf("expected not-listed error, got: %v", err)
	}
}

// TestApplySwapsOnValidChecksum is the happy-path swap, isolated from the test
// runner by re-execing a child whose executable is a throwaway file. The child
// (LATTICE_UPDATE_TEST_CHILD=1) calls Apply against the mock base and exits 0 only
// if the swap landed the verified bytes onto its own binary path.
func TestApplySwapsOnValidChecksum(t *testing.T) {
	if os.Getenv("LATTICE_UPDATE_TEST_CHILD") == "1" {
		runChildSwap()
		return
	}
	if runtime.GOOS == "windows" {
		t.Skip("child-exec swap harness targets unix rename semantics")
	}

	body := []byte("VERIFIED-NEW-BINARY-CONTENT")
	base := mockRelease(t, body, sum(body))

	// Build a throwaway "current binary": a copy of this test executable placed in
	// a temp dir, so Apply's os.Executable→EvalSymlinks resolves to a file we own
	// and the atomic rename swaps THAT, never the real go test binary.
	self, err := os.Executable()
	if err != nil {
		t.Fatalf("os.Executable: %v", err)
	}
	dir := t.TempDir()
	fake := filepath.Join(dir, "lattice")
	copyFile(t, self, fake)

	cmd := exec.Command(fake, "-test.run=TestApplySwapsOnValidChecksum")
	cmd.Env = append(os.Environ(),
		"LATTICE_UPDATE_TEST_CHILD=1",
		"LATTICE_UPDATE_TEST_BASE="+base,
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("child swap failed: %v\n%s", err, out)
	}

	// After the swap, the fake binary's bytes must equal the verified asset.
	got, err := os.ReadFile(fake)
	if err != nil {
		t.Fatalf("read swapped binary: %v", err)
	}
	if string(got) != string(body) {
		t.Fatalf("swapped binary mismatch:\n got %q\n want %q", got, body)
	}
}

// runChildSwap is the subprocess body: Apply against the mock base, then signal
// success purely via exit code (the parent verifies the on-disk bytes).
func runChildSwap() {
	base := os.Getenv("LATTICE_UPDATE_TEST_BASE")
	if _, err := Apply(context.Background(), Options{Base: base}); err != nil {
		fmt.Fprintf(os.Stderr, "child Apply failed: %v\n", err)
		os.Exit(1)
	}
	os.Exit(0)
}

func copyFile(t *testing.T, src, dst string) {
	t.Helper()
	b, err := os.ReadFile(src)
	if err != nil {
		t.Fatalf("read %s: %v", src, err)
	}
	if err := os.WriteFile(dst, b, 0o755); err != nil {
		t.Fatalf("write %s: %v", dst, err)
	}
}
