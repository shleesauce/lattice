package hub

import (
	"testing"
	"time"

	"github.com/shleesauce/lattice/internal/proto"
	"github.com/shleesauce/lattice/internal/transcript"
)

// firstPRURL finds a GitHub PR URL in block text, ignores non-PR github links, and
// returns the most-recent PR when several appear.
func TestFirstPRURL(t *testing.T) {
	none := firstPRURL([]transcript.Block{
		{Text: "no links here"},
		{Text: "see https://github.com/owner/repo/issues/9 (issue, not a PR)"},
		{Text: "https://github.com/owner/repo/commit/abc123"},
	})
	if none != "" {
		t.Errorf("non-PR links should not match, got %q", none)
	}

	got := firstPRURL([]transcript.Block{
		{Text: "opened https://github.com/shleesauce/lattice/pull/12"},
		{Text: "and then https://github.com/shleesauce/lattice/pull/34 superseded it"},
	})
	// newest-last scan ⇒ pull/34 wins
	if got != "https://github.com/shleesauce/lattice/pull/34" {
		t.Errorf("expected newest PR, got %q", got)
	}

	// trailing path is trimmed to canonical /pull/<n>
	trim := firstPRURL([]transcript.Block{
		{Text: "PR: https://github.com/o/r/pull/7/files#diff-abc"},
	})
	if trim != "https://github.com/o/r/pull/7" {
		t.Errorf("trailing path should trim, got %q", trim)
	}
}

// SetSessionPRURLIfEmpty is a one-shot CAS: the first call sets + returns true, a
// second (with any URL) is a no-op returning false — the structural dedupe behind
// the single "PR opened" push.
func TestSetSessionPRURLIfEmpty(t *testing.T) {
	st := testStore(t)
	now := time.Now()
	if err := st.UpsertSession(SessionRecord{
		ID: "pr1", AgentID: "a1", Kind: string(proto.SessionClaude),
		Status: proto.SessionLive, Scope: "project", CreatedAt: now, LastActiveAt: now,
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	set, err := st.SetSessionPRURLIfEmpty("pr1", "https://github.com/o/r/pull/1", now)
	if err != nil || !set {
		t.Fatalf("first set: set=%v err=%v", set, err)
	}
	rec, _, _ := st.GetSession("pr1")
	if rec.PRURL != "https://github.com/o/r/pull/1" {
		t.Fatalf("PR not persisted: %q", rec.PRURL)
	}

	set2, err := st.SetSessionPRURLIfEmpty("pr1", "https://github.com/o/r/pull/2", now)
	if err != nil {
		t.Fatalf("second set err: %v", err)
	}
	if set2 {
		t.Fatal("second set should be a no-op (already has a PR)")
	}
	rec, _, _ = st.GetSession("pr1")
	if rec.PRURL != "https://github.com/o/r/pull/1" {
		t.Fatalf("PR overwritten on dedupe: %q", rec.PRURL)
	}
}

// PRURL survives a re-adopt/resume (INSERT-only, absent from the conflict UPDATE).
func TestPRURLPersistsAcrossReadopt(t *testing.T) {
	st := testStore(t)
	now := time.Now()
	rec := SessionRecord{
		ID: "pr2", AgentID: "a1", Kind: string(proto.SessionClaude),
		Status: proto.SessionLive, Scope: "project",
		PRURL:     "https://github.com/o/r/pull/5",
		CreatedAt: now, LastActiveAt: now,
	}
	if err := st.UpsertSession(rec); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	readopt := rec
	readopt.PRURL = ""
	readopt.Status = proto.SessionOrphaned
	if err := st.UpsertSession(readopt); err != nil {
		t.Fatalf("re-upsert: %v", err)
	}
	got, _, _ := st.GetSession("pr2")
	if got.PRURL != "https://github.com/o/r/pull/5" {
		t.Fatalf("PR wiped on re-adopt: %q", got.PRURL)
	}
}
