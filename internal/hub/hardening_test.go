package hub

import (
	"errors"
	"net/http"
	"testing"
	"time"
)

// --- Store.ReapAuditLog: row-count cap on the otherwise-unbounded audit_log ---

func TestReapAuditLog(t *testing.T) {
	st := testStore(t)
	// 12 rows → ids 1..12 (AUTOINCREMENT, monotonic insert order).
	for i := 0; i < 12; i++ {
		if _, err := st.db.Exec(
			`INSERT INTO audit_log (session_id, agent_id, event_type, tool_name, detail_json, at)
			 VALUES (?,?,?,?,?,?)`,
			"sess", "mini", "tool_use", "Bash", "{}",
			time.Now().UTC().Format(time.RFC3339Nano),
		); err != nil {
			t.Fatalf("insert %d: %v", i, err)
		}
	}

	n, err := st.ReapAuditLog(5)
	if err != nil {
		t.Fatalf("reap: %v", err)
	}
	if n != 7 {
		t.Fatalf("deleted=%d want 7", n)
	}

	// Exactly the 5 newest rows survive (highest ids 8..12).
	var count, minID int
	if err := st.db.QueryRow(`SELECT COUNT(*), COALESCE(MIN(id),0) FROM audit_log`).
		Scan(&count, &minID); err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 5 {
		t.Fatalf("remaining=%d want 5", count)
	}
	if minID != 8 {
		t.Errorf("min surviving id=%d want 8 (keeps newest by id)", minID)
	}

	// Idempotent: reaping again at the same cap deletes nothing.
	if n, err := st.ReapAuditLog(5); err != nil || n != 0 {
		t.Errorf("second reap: n=%d err=%v want 0,nil", n, err)
	}
	// keep >= rowcount is a no-op.
	if n, err := st.ReapAuditLog(100); err != nil || n != 0 {
		t.Errorf("reap keep>rows: n=%d err=%v want 0,nil", n, err)
	}
}

// --- Store.ReapRevokedEnrollTokens: age-cutoff drop, conservative on purpose ---

func TestReapRevokedEnrollTokens(t *testing.T) {
	st := testStore(t)
	now := time.Now()

	mustCreate := func(tok string) {
		if err := st.CreateEnrollToken(tok, tok); err != nil {
			t.Fatalf("create %s: %v", tok, err)
		}
	}
	setRevoked := func(tok string, at time.Time) {
		if _, err := st.db.Exec(`UPDATE enroll_tokens SET revoked_at=? WHERE token=?`,
			at.Unix(), tok); err != nil {
			t.Fatalf("set revoked %s: %v", tok, err)
		}
	}

	mustCreate("live") // revoked_at=0 → must NEVER be deleted
	mustCreate("old-revoked")
	setRevoked("old-revoked", now.Add(-48*time.Hour))
	mustCreate("fresh-revoked")
	setRevoked("fresh-revoked", now.Add(-1*time.Hour))

	// Cutoff = 24h ago: only old-revoked predates it.
	n, err := st.ReapRevokedEnrollTokens(now.Add(-24 * time.Hour))
	if err != nil {
		t.Fatalf("reap: %v", err)
	}
	if n != 1 {
		t.Fatalf("deleted=%d want 1", n)
	}

	surviving := map[string]bool{}
	recs, err := st.ListEnrollTokens()
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	for _, r := range recs {
		surviving[r.Token] = true
	}
	if surviving["old-revoked"] {
		t.Error("old-revoked should be deleted")
	}
	if !surviving["live"] {
		t.Error("live token (revoked_at=0) must never be deleted")
	}
	if !surviving["fresh-revoked"] {
		t.Error("recently-revoked token must stay visible")
	}
}

// --- Store.RevokeEnrollToken: no-op revokes surface errNoEnrollToken (→404) ---

func TestRevokeEnrollTokenNoOp(t *testing.T) {
	st := testStore(t)
	if err := st.CreateEnrollToken("tok", "label"); err != nil {
		t.Fatalf("create: %v", err)
	}

	if err := st.RevokeEnrollToken("tok"); err != nil {
		t.Fatalf("first revoke should succeed: %v", err)
	}
	if err := st.RevokeEnrollToken("tok"); !errors.Is(err, errNoEnrollToken) {
		t.Errorf("re-revoke: err=%v want errNoEnrollToken", err)
	}
	if err := st.RevokeEnrollToken("never-existed"); !errors.Is(err, errNoEnrollToken) {
		t.Errorf("unknown revoke: err=%v want errNoEnrollToken", err)
	}
}

// --- loginLimiter.sweep: drops stale-only buckets, keeps any with a live entry ---

func TestLoginLimiterSweep(t *testing.T) {
	l := newLoginLimiter()
	now := time.Now()

	// All entries older than the window → swept.
	l.fails["stale"] = []time.Time{now.Add(-2 * loginWindow), now.Add(-90 * time.Minute)}
	// One in-window entry → kept (still counts toward the limit).
	l.fails["mixed"] = []time.Time{now.Add(-2 * loginWindow), now.Add(-10 * time.Second)}
	// Fresh → kept.
	l.fails["live"] = []time.Time{now.Add(-1 * time.Minute)}

	l.sweep()

	if _, ok := l.fails["stale"]; ok {
		t.Error("stale-only bucket should be swept")
	}
	if _, ok := l.fails["mixed"]; !ok {
		t.Error("bucket with an in-window entry must survive")
	}
	if _, ok := l.fails["live"]; !ok {
		t.Error("live bucket must survive")
	}
}

// --- canonicalHubURL: configured URL beats the attacker-controllable Host ---

func TestCanonicalHubURL(t *testing.T) {
	req := &http.Request{Host: "spoofed.evil:7400", Header: http.Header{}}

	// Operator-configured canonical URL is used verbatim, ignoring request Host.
	h := &Hub{hubURL: "http://real-hub.ts.net:7400"}
	if got := h.canonicalHubURL(req); got != "http://real-hub.ts.net:7400" {
		t.Errorf("configured: got %q, must not trust request Host", got)
	}

	// Unconfigured: falls back to the request-derived URL (stock LAN/tailnet hub).
	h2 := &Hub{}
	if got := h2.canonicalHubURL(req); got != "http://spoofed.evil:7400" {
		t.Errorf("fallback: got %q want request-derived", got)
	}
	// X-Forwarded-Proto is honored only on the fallback path.
	req.Header.Set("X-Forwarded-Proto", "https")
	if got := h2.canonicalHubURL(req); got != "https://spoofed.evil:7400" {
		t.Errorf("fallback https: got %q", got)
	}
}

// --- hubHostPort: strips scheme, tolerates a missing one ---

func TestHubHostPort(t *testing.T) {
	for in, want := range map[string]string{
		"http://host:7400":  "host:7400",
		"https://host:7400": "host:7400",
		"host:7400":         "host:7400", // no scheme → unchanged
		"http://host":       "host",
	} {
		if got := hubHostPort(in); got != want {
			t.Errorf("hubHostPort(%q)=%q want %q", in, got, want)
		}
	}
}

// --- Registry.liveAgentCount: in-memory count, no DB round-trip ---

func TestLiveAgentCount(t *testing.T) {
	r := &Registry{agents: map[string]*agentConn{}}
	if got := r.liveAgentCount(); got != 0 {
		t.Errorf("empty: got %d want 0", got)
	}
	r.agents["a"] = &agentConn{id: "a"}
	r.agents["b"] = &agentConn{id: "b"}
	if got := r.liveAgentCount(); got != 2 {
		t.Errorf("got %d want 2", got)
	}
}

// --- isMasterToken: exact, constant-time match against the master token ---

func TestIsMasterToken(t *testing.T) {
	h := &Hub{token: "master-secret"}
	if !h.isMasterToken("master-secret") {
		t.Error("exact master token must match")
	}
	if h.isMasterToken("wrong") {
		t.Error("non-master token must not match")
	}
	if h.isMasterToken("") {
		t.Error("empty presented token must not match")
	}
}
