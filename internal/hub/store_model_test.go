package hub

import (
	"testing"
	"time"

	"github.com/shleesauce/lattice/internal/proto"
)

// A session's launched model round-trips through the store, and survives a
// re-upsert (re-adopt/resume): model is INSERT-only — the conflict UPDATE must not
// overwrite it with a blank, the way notify_on_idle is preserved (D20 identity).
func TestSessionModelPersists(t *testing.T) {
	st := testStore(t)
	now := time.Now()
	rec := SessionRecord{
		ID: "sm1", AgentID: "a1", Kind: string(proto.SessionClaude),
		Status: proto.SessionLive, Scope: "project",
		Model:     "claude-opus-4-8[1m]",
		CreatedAt: now, LastActiveAt: now,
	}
	if err := st.UpsertSession(rec); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	got, ok, err := st.GetSession("sm1")
	if err != nil || !ok {
		t.Fatalf("get: ok=%v err=%v", ok, err)
	}
	if got.Model != "claude-opus-4-8[1m]" {
		t.Fatalf("model not persisted: got %q", got.Model)
	}

	// Re-adopt rebuilds the record with a blank model (the create path only carries
	// it on first insert). The conflict UPDATE must NOT wipe the stored value.
	readopt := rec
	readopt.Model = ""
	readopt.Status = proto.SessionOrphaned
	if err := st.UpsertSession(readopt); err != nil {
		t.Fatalf("re-upsert: %v", err)
	}
	got, _, _ = st.GetSession("sm1")
	if got.Model != "claude-opus-4-8[1m]" {
		t.Fatalf("model wiped on re-adopt: got %q", got.Model)
	}
	if got.Status != proto.SessionOrphaned {
		t.Fatalf("status not updated on re-adopt: got %q", got.Status)
	}
}
