package uninstall

import (
	"path/filepath"
	"strings"
	"testing"
)

// planFor must produce the exact labels + paths the installers create, per OS —
// this is the contract that keeps uninstall in lockstep with get.sh/get.ps1.
func TestPlanForDarwin(t *testing.T) {
	p := planFor("darwin", "/Users/x", "", 501)
	wantLabels := []string{"sh.lattice.hub", "sh.lattice.agent"}
	gotLabels := labels(p)
	if strings.Join(gotLabels, ",") != strings.Join(wantLabels, ",") {
		t.Errorf("labels=%v want %v", gotLabels, wantLabels)
	}
	for _, s := range p.services {
		if s.kind != "launchd" {
			t.Errorf("%s kind=%s want launchd", s.label, s.kind)
		}
		if !strings.HasPrefix(s.file, "/Users/x/Library/LaunchAgents/") || !strings.HasSuffix(s.file, ".plist") {
			t.Errorf("unexpected plist path: %s", s.file)
		}
	}
	if len(p.dataDirs) != 1 || p.dataDirs[0] != "/Users/x/.lattice" {
		t.Errorf("dataDirs=%v want [/Users/x/.lattice]", p.dataDirs)
	}
}

func TestPlanForLinux(t *testing.T) {
	p := planFor("linux", "/home/x", "", 1000)
	if labels(p)[0] != "lattice-hub" || labels(p)[1] != "lattice-agent" {
		t.Errorf("labels=%v", labels(p))
	}
	for _, s := range p.services {
		if s.kind != "systemd" {
			t.Errorf("%s kind=%s want systemd", s.label, s.kind)
		}
		if !strings.HasPrefix(s.file, "/home/x/.config/systemd/user/") || !strings.HasSuffix(s.file, ".service") {
			t.Errorf("unexpected unit path: %s", s.file)
		}
	}
	if len(p.dataDirs) != 1 || p.dataDirs[0] != "/home/x/.lattice" {
		t.Errorf("dataDirs=%v", p.dataDirs)
	}
}

func TestPlanForWindows(t *testing.T) {
	p := planFor("windows", `C:\Users\x`, `C:\Users\x\AppData\Local`, -1)
	if labels(p)[0] != "LatticeHub" || labels(p)[1] != "LatticeAgent" {
		t.Errorf("labels=%v", labels(p))
	}
	for _, s := range p.services {
		if s.kind != "schtask" || s.file != "" {
			t.Errorf("%s kind=%s file=%q want schtask/empty-file", s.label, s.kind, s.file)
		}
	}
	// Windows removes BOTH the profile data dir and the LOCALAPPDATA binary dir.
	// Build expected paths with filepath.Join so the assertion is separator-
	// agnostic (the test may run on a non-Windows host in CI).
	wantProfile := filepath.Join(`C:\Users\x`, ".lattice")
	wantBinDir := filepath.Join(`C:\Users\x\AppData\Local`, "Lattice")
	if len(p.dataDirs) != 2 || p.dataDirs[0] != wantProfile || p.dataDirs[1] != wantBinDir {
		t.Errorf("dataDirs=%v want [%s %s]", p.dataDirs, wantProfile, wantBinDir)
	}
}

// Without LOCALAPPDATA set, Windows still removes the profile data dir (no panic,
// no empty path appended).
func TestPlanForWindowsNoLocalAppData(t *testing.T) {
	p := planFor("windows", `C:\Users\x`, "", -1)
	want := filepath.Join(`C:\Users\x`, ".lattice")
	if len(p.dataDirs) != 1 || p.dataDirs[0] != want {
		t.Errorf("dataDirs=%v want only %s", p.dataDirs, want)
	}
}

func labels(p plan) []string {
	out := make([]string, len(p.services))
	for i, s := range p.services {
		out[i] = s.label
	}
	return out
}
