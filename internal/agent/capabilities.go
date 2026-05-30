package agent

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/dylanstoryyy/lattice/internal/proto"
)

// capProbeTimeout bounds each `--version` probe so a wedged binary can't stall
// the register/heartbeat assembly path.
const capProbeTimeout = 3 * time.Second

// detectCapabilities probes for the claude + node binaries and their versions.
// It feeds the placement hard filter (D19), so it must reflect what the agent
// can ACTUALLY launch — including under the Windows Scheduled-Task service whose
// PATH differs from an interactive shell (claude.exe may not be on PATH there).
func detectCapabilities(ctx context.Context) proto.Capabilities {
	var c proto.Capabilities

	if path := resolveClaude(); path != "" {
		c.ClaudeInstalled = true
		c.ClaudeVersion = probeVersion(ctx, path, "--version")
	}
	if path, err := exec.LookPath("node"); err == nil {
		c.NodeInstalled = true
		c.NodeVersion = probeVersion(ctx, path, "--version")
	}
	// IDE milestone (D28/D30): can this agent host an embedded editor?
	if path := resolveCodeServer(); path != "" {
		c.CodeServerInstalled = true
		c.CodeServerVersion = probeCodeServerVersion(ctx, path)
	}
	c.WSLAvailable = detectWSL()
	return c
}

// resolveClaude finds the claude binary. It tries PATH first, then falls back to
// well-known install locations because the Windows service context (and some
// login-shell-only PATH setups) won't have claude on PATH.
func resolveClaude() string {
	name := "claude"
	if runtime.GOOS == "windows" {
		name = "claude.exe"
	}
	if path, err := exec.LookPath(name); err == nil {
		return path
	}
	for _, cand := range claudeFallbackPaths() {
		if fi, err := os.Stat(cand); err == nil && !fi.IsDir() {
			return cand
		}
	}
	return ""
}

// claudeFallbackPaths returns OS-specific common install locations for the
// claude CLI to check when PATH lookup fails.
func claudeFallbackPaths() []string {
	home, _ := os.UserHomeDir()
	if home == "" {
		return nil
	}
	if runtime.GOOS == "windows" {
		return []string{
			filepath.Join(home, ".local", "bin", "claude.exe"),
			filepath.Join(home, "AppData", "Local", "Programs", "claude", "claude.exe"),
		}
	}
	return []string{
		filepath.Join(home, ".local", "bin", "claude"),
		"/opt/homebrew/bin/claude",
		"/usr/local/bin/claude",
	}
}

// probeVersion runs `<bin> <flag>` with a short timeout and returns the trimmed
// first line, or "" on any failure.
func probeVersion(ctx context.Context, bin, flag string) string {
	pctx, cancel := context.WithTimeout(ctx, capProbeTimeout)
	defer cancel()
	out, err := exec.CommandContext(pctx, bin, flag).Output()
	if err != nil {
		return ""
	}
	line := strings.TrimSpace(string(out))
	if i := strings.IndexByte(line, '\n'); i >= 0 {
		line = strings.TrimSpace(line[:i])
	}
	return line
}
