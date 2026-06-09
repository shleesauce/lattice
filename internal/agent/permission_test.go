package agent

import (
	"strings"
	"testing"

	"github.com/shleesauce/lattice/internal/proto"
)

// permissionMode passes through claude's known modes and defaults anything else to
// bypassPermissions — so a bad/empty value can never reach the launch.
func TestPermissionMode(t *testing.T) {
	for _, m := range []string{"default", "acceptEdits", "plan", "auto", "bypassPermissions", "dontAsk"} {
		if got := permissionMode(m); got != m {
			t.Errorf("permissionMode(%q)=%q want pass-through", m, got)
		}
	}
	for _, bad := range []string{"", "garbage", "ACCEPTEDITS", "--dangerous"} {
		if got := permissionMode(bad); got != "bypassPermissions" {
			t.Errorf("permissionMode(%q)=%q want bypassPermissions default", bad, got)
		}
	}
}

// claudeCommand must put the chosen --permission-mode into the launch argv (both
// the fresh and resume paths), and default to bypassPermissions when none is set.
func TestClaudeCommandPermissionMode(t *testing.T) {
	cases := []struct {
		name string
		p    proto.SessionCreatePayload
		want string
	}{
		{"fresh+plan", proto.SessionCreatePayload{SessionID: "s1", PermissionMode: "plan"}, "plan"},
		{"resume+acceptEdits", proto.SessionCreatePayload{ResumeID: "c1", PermissionMode: "acceptEdits"}, "acceptEdits"},
		{"empty→bypass", proto.SessionCreatePayload{SessionID: "s1"}, "bypassPermissions"},
		{"invalid→bypass", proto.SessionCreatePayload{SessionID: "s1", PermissionMode: "nope"}, "bypassPermissions"},
	}
	for _, c := range cases {
		_, args := claudeCommand(c.p)
		joined := strings.Join(args, " ")
		if !strings.Contains(joined, "--permission-mode") {
			t.Errorf("%s: argv missing --permission-mode: %v", c.name, args)
		}
		if !strings.Contains(joined, c.want) {
			t.Errorf("%s: argv missing mode %q: %v", c.name, c.want, args)
		}
	}
}
