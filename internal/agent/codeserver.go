package agent

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
)

// resolveCodeServer finds the code-server binary (the embedded editor core, D26).
// PATH first, then the common Homebrew / official-installer locations, because a
// launchd/Scheduled-Task service PATH differs from an interactive shell. Empty
// string ⇒ not installed, and placement (D19) excludes this agent from editor
// sessions. P1 is per-node install (decided 2026-05-30); a hub-served tarball
// (D28) is a clean future addition that would write into one of these paths.
func resolveCodeServer() string {
	if path, err := exec.LookPath("code-server"); err == nil {
		return path
	}
	for _, cand := range codeServerFallbackPaths() {
		if fi, err := os.Stat(cand); err == nil && !fi.IsDir() {
			return cand
		}
	}
	return ""
}

// codeServerFallbackPaths returns common install locations to probe when PATH
// lookup fails. On Windows code-server runs INSIDE WSL2 (D30), so the native
// PATH won't have it — WSL detection (detectWSL) gates the editor there instead.
func codeServerFallbackPaths() []string {
	home, _ := os.UserHomeDir()
	cands := []string{
		"/opt/homebrew/bin/code-server",
		"/usr/local/bin/code-server",
	}
	if home != "" {
		cands = append(cands, filepath.Join(home, ".local", "bin", "code-server"))
	}
	return cands
}

// detectWSL reports whether WSL2 is available (Windows only, D30). The editor on
// Windows runs code-server inside a WSL distro so it can reach /mnt/c paths.
func detectWSL() bool {
	if runtime.GOOS != "windows" {
		return false
	}
	_, err := exec.LookPath("wsl.exe")
	return err == nil
}

// probeCodeServerVersion returns code-server's version line (e.g.
// "4.112.0 <hash> with Code 1.112.0"), trimmed to the first line, or "".
func probeCodeServerVersion(ctx context.Context, bin string) string {
	return probeVersion(ctx, bin, "--version")
}
