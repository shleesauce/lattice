package hub

import (
	"strings"
	"testing"
)

// enrollOneLiners must keep the token in the environment (never argv) on both
// platforms, and target the hub's install endpoints.
func TestEnrollOneLiners(t *testing.T) {
	unix, win := enrollOneLiners("http://hub:7400", "TOK")
	if !strings.Contains(unix, "LATTICE_TOKEN='TOK'") || !strings.Contains(unix, "/install.sh") {
		t.Errorf("unix one-liner wrong: %q", unix)
	}
	if strings.Contains(unix, "--token") {
		t.Errorf("token must not be on argv: %q", unix)
	}
	if !strings.Contains(win, "$env:LATTICE_TOKEN='TOK'") || !strings.Contains(win, "/install.ps1") {
		t.Errorf("windows one-liner wrong: %q", win)
	}
}

// tailscaleSetupOneLiners must hand back the official, idempotent install+up
// commands the dashboard shows as the mesh prerequisite.
func TestTailscaleSetupOneLiners(t *testing.T) {
	unix, win := tailscaleSetupOneLiners()
	for _, want := range []string{"tailscale.com/install.sh", "tailscale up"} {
		if !strings.Contains(unix, want) {
			t.Errorf("unix tailscale setup missing %q: %q", want, unix)
		}
	}
	for _, want := range []string{"tailscale.tailscale", "tailscale up"} {
		if !strings.Contains(win, want) {
			t.Errorf("windows tailscale setup missing %q: %q", want, win)
		}
	}
}
