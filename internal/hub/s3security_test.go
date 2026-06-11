package hub

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// A passwordless hub must STILL require the master token for RCE-class actions
// (exec/update/power) — the fail-closed fix. requirePrivileged is the gate.
func TestRequirePrivilegedPasswordlessRequiresToken(t *testing.T) {
	h := &Hub{token: testMasterToken}

	// No credential on a passwordless hub → blocked with 401 (the closed hole).
	rec := httptest.NewRecorder()
	if h.requirePrivileged(rec, httptest.NewRequest(http.MethodPost, "/api/update", nil)) {
		t.Fatal("passwordless + no token should be blocked")
	}
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("want 401, got %d", rec.Code)
	}

	// Master-token Bearer → allowed (the operator holds it).
	rec = httptest.NewRecorder()
	if !h.requirePrivileged(rec, privileged(httptest.NewRequest(http.MethodPost, "/api/update", nil))) {
		t.Fatal("master token should pass")
	}

	// A wrong Bearer is still blocked.
	rec = httptest.NewRecorder()
	bad := httptest.NewRequest(http.MethodPost, "/api/update", nil)
	bad.Header.Set("Authorization", "Bearer not-the-token")
	if h.requirePrivileged(rec, bad) {
		t.Fatal("wrong token should be blocked")
	}
}

// When admin auth IS enabled, requireAuth already gated the route, so
// requirePrivileged defers (passes) — it only adds protection on the auth-off path.
func TestRequirePrivilegedAuthEnabledDefers(t *testing.T) {
	h := &Hub{token: testMasterToken, adminPasswordHash: "bcrypt-hash-present"}
	rec := httptest.NewRecorder()
	if !h.requirePrivileged(rec, httptest.NewRequest(http.MethodPost, "/api/update", nil)) {
		t.Fatal("auth-enabled hub should defer to requireAuth (pass)")
	}
}

// The first-run setup wizard must reject a remote peer with no token (the
// unauthenticated-takeover window) but allow loopback or a token-bearing request.
func TestSetupAllowed(t *testing.T) {
	h := &Hub{token: testMasterToken}

	// Remote (httptest default RemoteAddr 192.0.2.1), no token → blocked.
	if h.setupAllowed(httptest.NewRequest(http.MethodPost, "/api/setup", nil)) {
		t.Fatal("remote + no token should be blocked")
	}
	// Remote + master token → allowed.
	if !h.setupAllowed(privileged(httptest.NewRequest(http.MethodPost, "/api/setup", nil))) {
		t.Fatal("remote + master token should pass")
	}
	// Loopback, no token → allowed (operator on the hub box).
	lr := httptest.NewRequest(http.MethodPost, "/api/setup", nil)
	lr.RemoteAddr = "127.0.0.1:5555"
	if !h.setupAllowed(lr) {
		t.Fatal("loopback should pass without a token")
	}
}

func TestRequestIsLoopback(t *testing.T) {
	lr := httptest.NewRequest(http.MethodGet, "/", nil)
	lr.RemoteAddr = "127.0.0.1:1"
	if !requestIsLoopback(lr) {
		t.Fatal("127.0.0.1 should be loopback")
	}
	rr := httptest.NewRequest(http.MethodGet, "/", nil) // 192.0.2.1:1234
	if requestIsLoopback(rr) {
		t.Fatal("192.0.2.1 should not be loopback")
	}
}

// rateLimiter caps per-IP requests in a window, isolates IPs, and is nil-safe.
func TestRateLimiter(t *testing.T) {
	l := newRateLimiter(3, time.Minute)
	for i := 0; i < 3; i++ {
		if !l.allow("1.2.3.4") {
			t.Fatalf("request %d under the cap should be allowed", i)
		}
	}
	if l.allow("1.2.3.4") {
		t.Fatal("4th request over the cap should be blocked")
	}
	if !l.allow("5.6.7.8") {
		t.Fatal("a different IP has its own bucket and should be allowed")
	}

	// Nil receiver allows everything and never panics (a Hub built without one).
	var n *rateLimiter
	if !n.allow("x") {
		t.Fatal("nil limiter should allow")
	}
	n.sweep()
}
