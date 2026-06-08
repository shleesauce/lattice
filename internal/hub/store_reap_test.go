package hub

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/shleesauce/lattice/internal/proto"
)

// testStore opens a throwaway SQLite store in a temp dir.
func testStore(t *testing.T) *Store {
	t.Helper()
	st, err := OpenStore(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	return st
}

func putSession(t *testing.T, st *Store, id, agentID, kind, status string, lastActive time.Time) {
	t.Helper()
	if err := st.UpsertSession(SessionRecord{
		ID: id, AgentID: agentID, Kind: kind, Status: status, Scope: "project",
		CreatedAt: lastActive, LastActiveAt: lastActive,
	}); err != nil {
		t.Fatalf("upsert %s: %v", id, err)
	}
}

func getStatus(t *testing.T, st *Store, id string) string {
	t.Helper()
	rec, _, err := st.GetSession(id)
	if err != nil {
		t.Fatalf("get %s: %v", id, err)
	}
	return rec.Status
}

// MarkAgentSessionsExitedExcept reaps an agent's running rows that the agent no
// longer reports (their process is gone), leaves the kept ones, never touches
// another agent's rows or a 'starting' row mid-create (F18).
func TestMarkAgentSessionsExitedExcept(t *testing.T) {
	st := testStore(t)
	now := time.Now()

	putSession(t, st, "keep", "mini", "claude", proto.SessionLive, now)          // still reported → keep
	putSession(t, st, "gone-live", "mini", "claude", proto.SessionLive, now)     // not reported → exit
	putSession(t, st, "gone-orph", "mini", "claude", proto.SessionOrphaned, now) // not reported → exit
	putSession(t, st, "starting", "mini", "claude", proto.SessionStarting, now)  // mid-create → leave
	putSession(t, st, "other", "agent-b", "claude", proto.SessionLive, now)      // other agent → untouched

	n, err := st.MarkAgentSessionsExitedExcept("mini", map[string]bool{"keep": true})
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if n != 2 {
		t.Fatalf("expected 2 reaped, got %d", n)
	}
	want := map[string]string{
		"keep":      proto.SessionLive,
		"gone-live": proto.SessionExited,
		"gone-orph": proto.SessionExited,
		"starting":  proto.SessionStarting,
		"other":     proto.SessionLive,
	}
	for id, ws := range want {
		if got := getStatus(t, st, id); got != ws {
			t.Errorf("%s: status=%q want %q", id, got, ws)
		}
	}
}

// A reconciled-exited session keeps its original last_active_at (not bumped to
// now), so the reaper measures its grace from when it actually died.
func TestReconcileKeepsLastActive(t *testing.T) {
	st := testStore(t)
	old := time.Now().Add(-2 * time.Hour)
	putSession(t, st, "dead", "mini", "terminal", proto.SessionLive, old)

	if _, err := st.MarkAgentSessionsExitedExcept("mini", map[string]bool{}); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	rec, _, _ := st.GetSession("dead")
	if rec.Status != proto.SessionExited {
		t.Fatalf("status=%q", rec.Status)
	}
	if rec.LastActiveAt.UTC().Sub(old.UTC()) > time.Second {
		t.Errorf("last_active_at was bumped: got %v want ~%v", rec.LastActiveAt, old)
	}
}

// ArchiveExitedBefore auto-archives only exited sessions past the grace cutoff,
// and leaves live / already-archived / trashed / freshly-exited rows alone (F18).
func TestArchiveExitedBefore(t *testing.T) {
	st := testStore(t)
	now := time.Now()
	stale := now.Add(-30 * time.Minute) // past a 10-min grace
	fresh := now.Add(-1 * time.Minute)  // inside grace

	putSession(t, st, "stale-exit", "mini", "claude", proto.SessionExited, stale) // → archived
	putSession(t, st, "fresh-exit", "mini", "claude", proto.SessionExited, fresh) // grace → stays
	putSession(t, st, "live", "mini", "claude", proto.SessionLive, stale)         // not exited → stays
	// already trashed exited: must not be touched
	putSession(t, st, "trashed", "mini", "claude", proto.SessionExited, stale)
	if err := st.SetSessionDeleted("trashed", true, stale); err != nil {
		t.Fatalf("trash: %v", err)
	}

	n, err := st.ArchiveExitedBefore(now.Add(-10 * time.Minute))
	if err != nil {
		t.Fatalf("reap: %v", err)
	}
	if n != 1 {
		t.Fatalf("expected 1 archived, got %d", n)
	}
	for id, wantArch := range map[string]bool{
		"stale-exit": true,
		"fresh-exit": false,
		"live":       false,
		"trashed":    false, // stays in Trash, not flipped to Archived
	} {
		rec, _, _ := st.GetSession(id)
		if rec.Archived != wantArch {
			t.Errorf("%s: archived=%v want %v", id, rec.Archived, wantArch)
		}
	}
}
