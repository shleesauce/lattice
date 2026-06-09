package agent

import (
	"strings"
	"testing"

	"github.com/shleesauce/lattice/internal/proto"
)

// claudeModel passes through allow-listed model ids (including the [1m] 1M-context
// form) and drops anything else to "" so a bad value can't reach the launch.
func TestClaudeModel(t *testing.T) {
	for _, m := range []string{
		"claude-opus-4-8", "claude-opus-4-8[1m]", "claude-sonnet-4-6",
		"claude-haiku-4-5", "claude-fable-5", "claude-fable-5[1m]",
		"claude-opus-4-7", "claude-opus-4-7[1m]", "claude-opus-4-6",
	} {
		if got := claudeModel(m); got != m {
			t.Errorf("claudeModel(%q)=%q want pass-through", m, got)
		}
	}
	for _, bad := range []string{"", "opus", "claude-opus-9", "--dangerous", "claude-opus-4-8[2m]"} {
		if got := claudeModel(bad); got != "" {
			t.Errorf("claudeModel(%q)=%q want dropped to \"\"", bad, got)
		}
	}
	// whitespace is trimmed before the allow-list check
	if got := claudeModel("  claude-opus-4-8[1m]  "); got != "claude-opus-4-8[1m]" {
		t.Errorf("claudeModel trim: got %q", got)
	}
}

// claudeCommand must put a chosen --model into the launch argv on both the fresh
// and resume paths, omit --model entirely for an unknown/empty model, and add
// --effort low only when FastMode is set.
func TestClaudeCommandModel(t *testing.T) {
	cases := []struct {
		name      string
		p         proto.SessionCreatePayload
		wantModel string // "" ⇒ expect NO --model
		wantFast  bool
	}{
		{"fresh+opus1m", proto.SessionCreatePayload{SessionID: "s1", Model: "claude-opus-4-8[1m]"}, "claude-opus-4-8[1m]", false},
		{"resume+sonnet", proto.SessionCreatePayload{ResumeID: "c1", Model: "claude-sonnet-4-6"}, "claude-sonnet-4-6", false},
		{"empty→no-model", proto.SessionCreatePayload{SessionID: "s1"}, "", false},
		{"invalid→no-model", proto.SessionCreatePayload{SessionID: "s1", Model: "bogus"}, "", false},
		{"fast-mode", proto.SessionCreatePayload{SessionID: "s1", Model: "claude-opus-4-8", FastMode: true}, "claude-opus-4-8", true},
	}
	for _, c := range cases {
		_, args := claudeCommand(c.p)
		joined := strings.Join(args, " ")
		hasModel := strings.Contains(joined, "--model")
		if c.wantModel == "" {
			if hasModel {
				t.Errorf("%s: argv should NOT contain --model: %v", c.name, args)
			}
		} else {
			if !hasModel {
				t.Errorf("%s: argv missing --model: %v", c.name, args)
			}
			if !strings.Contains(joined, c.wantModel) {
				t.Errorf("%s: argv missing model id %q: %v", c.name, c.wantModel, args)
			}
		}
		hasEffort := strings.Contains(joined, "--effort") && strings.Contains(joined, "low")
		if c.wantFast && !hasEffort {
			t.Errorf("%s: FastMode set but argv missing --effort low: %v", c.name, args)
		}
		if !c.wantFast && hasEffort {
			t.Errorf("%s: FastMode unset but argv has --effort low: %v", c.name, args)
		}
	}
}
