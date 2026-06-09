package hub

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/shleesauce/lattice/internal/proto"
)

// hookHub is testHub plus an initialized hook store (testHub predates Phase C).
func hookHub(t *testing.T) *Hub {
	h := testHub(t)
	h.hooks = newHookStore()
	return h
}

func postHook(t *testing.T, h *Hub, body string) int {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/hooks/state", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.handleHookState(rec, req)
	return rec.Code
}

// A Stop hook with a valid token audits a precise turn_done edge and returns 200.
// A bad token is accepted-and-ignored (200, no audit) so a probe learns nothing and
// claude is never blocked.
func TestHandleHookStateStop(t *testing.T) {
	h := hookHub(t)
	now := time.Now()
	if err := h.store.UpsertSession(SessionRecord{
		ID: "hs1", AgentID: "a1", Kind: string(proto.SessionClaude),
		Status: proto.SessionLive, Scope: "project", CreatedAt: now, LastActiveAt: now,
	}); err != nil {
		t.Fatalf("seed session: %v", err)
	}
	tok := h.mintHookToken("hs1", now)

	// Bad token → 200, no audit row.
	if code := postHook(t, h, `{"sessionId":"hs1","token":"WRONG","event":"stop"}`); code != http.StatusOK {
		t.Fatalf("bad token: status=%d want 200", code)
	}
	if n := auditCount(t, h, "hs1"); n != 0 {
		t.Fatalf("bad token should not audit, got %d rows", n)
	}

	// Valid Stop → 200 + one turn_done audit row.
	if code := postHook(t, h, `{"sessionId":"hs1","token":"`+tok+`","event":"stop"}`); code != http.StatusOK {
		t.Fatalf("valid stop: status=%d want 200", code)
	}
	if n := auditCount(t, h, "hs1"); n != 1 {
		t.Fatalf("valid stop should audit once, got %d", n)
	}
}

// A Notification(permission_prompt) hook audits an awaiting_approval edge; a
// notification with a different matcher is ignored.
func TestHandleHookStateNotification(t *testing.T) {
	h := hookHub(t)
	now := time.Now()
	_ = h.store.UpsertSession(SessionRecord{
		ID: "hs2", AgentID: "a1", Kind: string(proto.SessionClaude),
		Status: proto.SessionLive, Scope: "project", CreatedAt: now, LastActiveAt: now,
	})
	tok := h.mintHookToken("hs2", now)

	// Non-permission notification → ignored (no audit).
	postHook(t, h, `{"sessionId":"hs2","token":"`+tok+`","event":"notification","matcher":"idle_prompt"}`)
	if n := auditCount(t, h, "hs2"); n != 0 {
		t.Fatalf("idle_prompt notification should not audit, got %d", n)
	}

	// permission_prompt → one awaiting_approval audit row.
	postHook(t, h, `{"sessionId":"hs2","token":"`+tok+`","event":"notification","matcher":"permission_prompt"}`)
	if n := auditCount(t, h, "hs2"); n != 1 {
		t.Fatalf("permission_prompt should audit once, got %d", n)
	}
}

// SessionEnd drops the hook token (so a later forged call with the same token is
// ignored) and is accepted with 200.
func TestHandleHookStateSessionEnd(t *testing.T) {
	h := hookHub(t)
	now := time.Now()
	_ = h.store.UpsertSession(SessionRecord{
		ID: "hs3", AgentID: "a1", Kind: string(proto.SessionClaude),
		Status: proto.SessionLive, Scope: "project", CreatedAt: now, LastActiveAt: now,
	})
	tok := h.mintHookToken("hs3", now)
	if !h.hooks.valid("hs3", tok) {
		t.Fatal("token should be valid before SessionEnd")
	}
	if code := postHook(t, h, `{"sessionId":"hs3","token":"`+tok+`","event":"session_end"}`); code != http.StatusOK {
		t.Fatalf("session_end: status=%d want 200", code)
	}
	if h.hooks.valid("hs3", tok) {
		t.Fatal("token should be dropped after SessionEnd")
	}
}

// Non-POST is rejected.
func TestHandleHookStateMethod(t *testing.T) {
	h := hookHub(t)
	rec := httptest.NewRecorder()
	h.handleHookState(rec, httptest.NewRequest(http.MethodGet, "/api/hooks/state", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("GET: status=%d want 405", rec.Code)
	}
}
