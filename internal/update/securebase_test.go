package update

import "testing"

// requireSecureBase HTTPS-pins the download channel: the asset and its SHA256SUMS
// share a base, so a plain-http base would let an in-transit attacker swap both.
func TestRequireSecureBase(t *testing.T) {
	if err := requireSecureBase("https://github.com/shleesauce/lattice/releases/latest/download"); err != nil {
		t.Fatalf("https base should pass: %v", err)
	}
	if err := requireSecureBase("http://127.0.0.1:8000"); err != nil {
		t.Fatalf("loopback http should pass (no in-transit surface): %v", err)
	}
	if err := requireSecureBase("http://localhost:8000/x"); err != nil {
		t.Fatalf("localhost http should pass: %v", err)
	}
	if err := requireSecureBase("http://example.com/x"); err == nil {
		t.Fatal("remote plain-http base must be rejected")
	}

	// Explicit operator opt-out (local mock-cascade testing over the tailnet).
	t.Setenv("LATTICE_DOWNLOAD_INSECURE", "1")
	if err := requireSecureBase("http://example.com/x"); err != nil {
		t.Fatalf("LATTICE_DOWNLOAD_INSECURE=1 should allow plain http: %v", err)
	}
}

func TestIsLoopbackHost(t *testing.T) {
	for _, h := range []string{"localhost", "127.0.0.1", "::1"} {
		if !isLoopbackHost(h) {
			t.Fatalf("%q should be loopback", h)
		}
	}
	for _, h := range []string{"example.com", "8.8.8.8", "100.64.0.1"} {
		if isLoopbackHost(h) {
			t.Fatalf("%q should not be loopback", h)
		}
	}
}
