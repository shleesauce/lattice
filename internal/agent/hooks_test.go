package agent

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/shleesauce/lattice/internal/proto"
)

// onceReset returns a fresh sync.Once so a test can force writeHookFiles to run
// again under a temp HOME (the production guard is process-wide).
func onceReset() sync.Once { return sync.Once{} }

// hookEnv only emits vars when all three pieces are present, and emits the exact
// names the notify script reads.
func TestHookEnv(t *testing.T) {
	if got := hookEnv("", "s1", "tok"); got != nil {
		t.Errorf("no hub URL should yield nil env, got %v", got)
	}
	if got := hookEnv("https://h", "s1", ""); got != nil {
		t.Errorf("no token should yield nil env, got %v", got)
	}
	env := hookEnv("https://hub.ts.net:7400", "sess-1", "captoken")
	joined := strings.Join(env, "\n")
	for _, want := range []string{
		"LATTICE_HUB_URL=https://hub.ts.net:7400",
		"LATTICE_SESSION_ID=sess-1",
		"LATTICE_HOOK_TOKEN=captoken",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("hookEnv missing %q in %v", want, env)
		}
	}
}

// hookSettingsPath writes a valid JSON settings file wiring the three hooks, under
// a HOME we control so the test never touches the real ~/.lattice.
func TestHookSettingsPath(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	// reset the once-guard so this test actually writes under the temp HOME
	hookSetupOnce = onceReset()

	p := hookSettingsPath()
	if p == "" {
		t.Fatal("hookSettingsPath returned empty")
	}
	if !strings.HasPrefix(p, filepath.Join(tmp, ".lattice", "hooks")) {
		t.Fatalf("settings written outside ~/.lattice/hooks: %s", p)
	}
	raw, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("read settings: %v", err)
	}
	var doc struct {
		Hooks map[string]json.RawMessage `json:"hooks"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("settings not valid JSON: %v\n%s", err, raw)
	}
	for _, ev := range []string{"Stop", "Notification", "SessionEnd"} {
		if _, ok := doc.Hooks[ev]; !ok {
			t.Errorf("settings missing %s hook", ev)
		}
	}
	// Must NEVER write anything under ~/.claude.
	if _, err := os.Stat(filepath.Join(tmp, ".claude", "settings.json")); !os.IsNotExist(err) {
		t.Errorf("hooks setup must not touch ~/.claude/settings.json")
	}
}

// claudeCommand adds --settings only when the hub wired a URL + token, and never
// otherwise (no hub URL ⇒ fall back to the idle heuristic, no --settings).
func TestClaudeCommandSettings(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	hookSetupOnce = onceReset()

	// Wired: HubURL + HookToken present.
	_, args := claudeCommand(proto.SessionCreatePayload{
		SessionID: "s1", HubURL: "https://hub.ts.net:7400", HookToken: "tok",
	})
	if !strings.Contains(strings.Join(args, " "), "--settings") {
		t.Errorf("wired session missing --settings: %v", args)
	}

	// Not wired: no HubURL ⇒ no --settings.
	_, args2 := claudeCommand(proto.SessionCreatePayload{SessionID: "s2"})
	if strings.Contains(strings.Join(args2, " "), "--settings") {
		t.Errorf("unwired session should NOT have --settings: %v", args2)
	}
}
