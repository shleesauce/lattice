package hub

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/shleesauce/lattice/internal/proto"
	"github.com/shleesauce/lattice/internal/transcript"
)

func TestDeriveTitle(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"simple", "fix the login bug", "Fix Login Bug"},
		{"drops_filler", "please can you help me refactor the auth module", "Refactor Auth Module"},
		{"clamps_words", "add a dark mode toggle to the settings page header bar", "Add Dark Mode Toggle Settings"},
		{"strips_markdown", "**Implement** the `useLiveResource` hook", "Implement UseLiveResource Hook"},
		{"strips_command_tag", "<command-name>build</command-name> the project now", "Build Project"},
		{"slash_command_only", "/clear", ""},
		{"slash_then_text", "/model opus\nwrite the migration script", "Write Migration Script"},
		{"fenced_code_skipped", "look at this:\n```go\nfunc main(){}\n```", "Look At This"},
		{"empty", "   ", ""},
		{"keeps_one_word", "deploy", "Deploy"},
		{"multiline_first_line", "Add rate limiting\nwith a token bucket", "Add Rate Limiting"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := deriveTitle(c.in); got != c.want {
				t.Fatalf("deriveTitle(%q) = %q, want %q", c.in, got, c.want)
			}
		})
	}
}

func TestClampTitle(t *testing.T) {
	long := strings.Repeat("word ", 30)
	got := clampTitle(long)
	if len(got) > maxTitleLen {
		t.Fatalf("clampTitle len = %d, want <= %d (%q)", len(got), maxTitleLen, got)
	}
	if strings.HasSuffix(got, " ") {
		t.Fatalf("clampTitle should trim trailing space: %q", got)
	}
	if got := clampTitle("  hello   world  "); got != "hello world" {
		t.Fatalf("clampTitle collapse = %q", got)
	}
}

func TestFirstUserText_SkipsToolResults(t *testing.T) {
	blocks := []transcript.Block{
		{Role: "user", Kind: "tool_result", Text: "command output noise"},
		{Role: "assistant", Kind: "text", Text: "an assistant turn"},
		{Role: "user", Kind: "text", Text: "the actual instruction"},
	}
	if got := firstUserText(blocks); got != "the actual instruction" {
		t.Fatalf("firstUserText = %q, want the actual instruction", got)
	}
}

// A manual rename via PATCH must persist AND lock the session so the auto-namer
// can never overwrite it on a later idle edge.
func TestRenameLocksAndPersists(t *testing.T) {
	h := testHub(t)
	if err := h.store.UpsertSession(SessionRecord{
		ID: "sess-rename", Kind: string(proto.SessionClaude), Status: proto.SessionLive,
		CreatedAt: time.Now(), LastActiveAt: time.Now(),
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	body, _ := json.Marshal(map[string]string{"title": "My Custom Name"})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPatch, "/api/sessions/sess-rename", bytes.NewReader(body))
	h.handleUpdateSession(rec, req, "sess-rename")
	if rec.Code != http.StatusOK {
		t.Fatalf("PATCH status = %d, body %s", rec.Code, rec.Body.String())
	}

	got, ok, _ := h.store.GetSession("sess-rename")
	if !ok || got.Title != "My Custom Name" {
		t.Fatalf("title not persisted: ok=%v title=%q", ok, got.Title)
	}
	if !h.autoNamer.isLocked("sess-rename") {
		t.Fatal("session should be title-locked after a manual rename")
	}

	// maybeAutoName must bail immediately on a locked session (never overwrites).
	h.maybeAutoName("agent-x", "sess-rename")
	got2, _, _ := h.store.GetSession("sess-rename")
	if got2.Title != "My Custom Name" {
		t.Fatalf("auto-namer clobbered a manual rename: %q", got2.Title)
	}
}

// An already-titled session is left alone by the auto-namer even without a lock
// (e.g. created with an explicit title).
func TestAutoNameSkipsTitledSession(t *testing.T) {
	h := testHub(t)
	if err := h.store.UpsertSession(SessionRecord{
		ID: "sess-titled", Kind: string(proto.SessionClaude), Title: "Preset", Status: proto.SessionLive,
		CreatedAt: time.Now(), LastActiveAt: time.Now(),
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	h.maybeAutoName("agent-x", "sess-titled")
	got, _, _ := h.store.GetSession("sess-titled")
	if got.Title != "Preset" {
		t.Fatalf("auto-namer should not touch a titled session: %q", got.Title)
	}
}
