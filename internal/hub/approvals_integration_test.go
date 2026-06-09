package hub

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/shleesauce/lattice/internal/proto"
)

// testHub builds a Hub backed by a fresh temp-DB store + empty registry/approvals,
// enough to drive the fire-and-forget HTTP + lifecycle paths without a live agent.
func testHub(t *testing.T) *Hub {
	t.Helper()
	store, err := OpenStore(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { store.Close() })
	return &Hub{store: store, registry: NewRegistry(), approvals: newApprovalStore(), autoNamer: newAutoNamer()}
}

func TestHandleApprovalInvalidNonce(t *testing.T) {
	h := testHub(t)
	rec := httptest.NewRecorder()
	h.handleApproval(rec, httptest.NewRequest(http.MethodPost, "/api/approvals/bogus?d=y", nil))
	if rec.Code != http.StatusGone {
		t.Fatalf("invalid nonce: status=%d want 410", rec.Code)
	}
}

func TestHandleApprovalEmptyNonce(t *testing.T) {
	h := testHub(t)
	rec := httptest.NewRecorder()
	h.handleApproval(rec, httptest.NewRequest(http.MethodPost, "/api/approvals/", nil))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("empty nonce: status=%d want 400", rec.Code)
	}
}

// A valid nonce whose agent is offline must report the conflict, NOT silently
// succeed — and the nonce is still consumed (single-shot) so a retry is rejected.
func TestHandleApprovalAgentOffline(t *testing.T) {
	h := testHub(t)
	nonce := h.approvals.mint("sess-1", "ghost-darwin", time.Now())

	rec := httptest.NewRecorder()
	h.handleApproval(rec, httptest.NewRequest(http.MethodPost, "/api/approvals/"+nonce+"?d=y", nil))
	if rec.Code != http.StatusConflict {
		t.Fatalf("offline agent: status=%d want 409", rec.Code)
	}
	// Nonce consumed even on the offline path: a second attempt is gone.
	rec2 := httptest.NewRecorder()
	h.handleApproval(rec2, httptest.NewRequest(http.MethodPost, "/api/approvals/"+nonce+"?d=y", nil))
	if rec2.Code != http.StatusGone {
		t.Fatalf("reused nonce: status=%d want 410", rec2.Code)
	}
}

func TestHandleApprovalMethodNotAllowed(t *testing.T) {
	h := testHub(t)
	rec := httptest.NewRecorder()
	h.handleApproval(rec, httptest.NewRequest(http.MethodDelete, "/api/approvals/whatever", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("DELETE: status=%d want 405", rec.Code)
	}
}

// handleSessionIdle audits both edges; the true edge for an opted-in session does
// not panic when ntfy is unconfigured (it simply sends nothing).
func TestHandleSessionIdleAudits(t *testing.T) {
	t.Setenv("LATTICE_NTFY_TOPIC", "") // notifications disabled
	h := testHub(t)
	now := time.Now()
	rec := SessionRecord{
		ID: "sess-1", Kind: string(proto.SessionClaude), AgentID: "studio-darwin",
		Status: proto.SessionLive, NotifyOnIdle: true, CreatedAt: now, LastActiveAt: now,
	}
	if err := h.store.UpsertSession(rec); err != nil {
		t.Fatalf("seed session: %v", err)
	}

	h.handleSessionIdle("studio-darwin", proto.SessionIdlePayload{SessionID: "sess-1", Idle: true, QuietMs: 60000})
	h.handleSessionIdle("studio-darwin", proto.SessionIdlePayload{SessionID: "sess-1", Idle: false})

	if n := auditCount(t, h, "sess-1"); n != 2 {
		t.Fatalf("audit rows = %d, want 2 (idle + active)", n)
	}
}

// onSessionExit audits the exit and suppresses the "finished" path for a hub-
// initiated close (expectExit) without erroring.
func TestOnSessionExitAuditsAndRespectsExpected(t *testing.T) {
	t.Setenv("LATTICE_NTFY_TOPIC", "")
	h := testHub(t)
	now := time.Now()
	rec := SessionRecord{
		ID: "sess-2", Kind: string(proto.SessionClaude), AgentID: "mbp-darwin",
		Status: proto.SessionLive, NotifyOnIdle: true, CreatedAt: now, LastActiveAt: now,
	}
	if err := h.store.UpsertSession(rec); err != nil {
		t.Fatalf("seed session: %v", err)
	}

	h.approvals.expectExit("sess-2", now) // user-initiated close
	h.onSessionExit("mbp-darwin", "sess-2")

	if n := auditCount(t, h, "sess-2"); n != 1 {
		t.Fatalf("audit rows = %d, want 1 (exit)", n)
	}
	// The expected marker is consumed, so a hypothetical re-exit would be treated
	// as unexpected — proving takeExpected cleared it.
	if h.approvals.takeExpected("sess-2") {
		t.Fatal("expected-exit marker not consumed by onSessionExit")
	}
}

func auditCount(t *testing.T, h *Hub, sessionID string) int {
	t.Helper()
	var n int
	if err := h.store.db.QueryRow(`SELECT COUNT(*) FROM audit_log WHERE session_id=?`, sessionID).Scan(&n); err != nil {
		t.Fatalf("count audit: %v", err)
	}
	return n
}
