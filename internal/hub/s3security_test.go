package hub

import (
	"net/http"
	"net/http/httptest"
	"strings"
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

// handleAgentSub must enforce each action's privilege + method from the agentActions
// table, centrally, so privilege can't be forgotten in a handler (the cause of the
// post-ship-audit passwordless-hub holes) and a new/unknown action fails closed.
func TestAgentSubActionGating(t *testing.T) {
	h := testHub(t) // passwordless (no admin password) → privileged actions need the token

	do := func(method, path string, priv bool) int {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(method, path, strings.NewReader("{}"))
		if priv {
			req = privileged(req)
		}
		h.handleAgentSub(rec, req)
		return rec.Code
	}

	// Privileged action (files) on a passwordless hub with NO token → 401, before any
	// agent round-trip. This is the hole the audit closed, now enforced centrally.
	if code := do(http.MethodGet, "/api/agents/studio/files", false); code != http.StatusUnauthorized {
		t.Fatalf("ungated privileged action: status=%d want 401", code)
	}
	// Unknown action → 404 (fail closed: a new sub-action can't ship ungated).
	if code := do(http.MethodPost, "/api/agents/studio/bogus", true); code != http.StatusNotFound {
		t.Fatalf("unknown action: status=%d want 404", code)
	}
	// Method is enforced from the table: wake is POST-only, so GET → 405.
	if code := do(http.MethodGet, "/api/agents/studio/wake", true); code != http.StatusMethodNotAllowed {
		t.Fatalf("wrong method: status=%d want 405", code)
	}
	// A non-privileged action (wake) is NOT blocked by the privilege gate even with no
	// token: it reaches handleWake, which 404s an unknown target (proves it wasn't 401'd).
	if code := do(http.MethodPost, "/api/agents/ghost/wake", false); code != http.StatusNotFound {
		t.Fatalf("non-privileged action should reach the handler (404 unknown target), got %d", code)
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
