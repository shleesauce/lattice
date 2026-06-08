package agent

import (
	"context"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/shleesauce/lattice/internal/proto"
)

// capProbeTimeout bounds each `--version` probe so a wedged binary can't stall
// the register/heartbeat assembly path.
const capProbeTimeout = 3 * time.Second

// capCacheTTL is how long a capability probe result is reused before re-probing.
// Capabilities (claude/node/code-server install + auth, WSL) change rarely, but
// gatherMetrics runs every heartbeat (5s); without this cache every heartbeat
// would spawn up to three `--version` subprocesses + a WSL probe per agent,
// forever. A 5-minute refresh keeps placement state fresh enough (D19) at a tiny
// fraction of the process churn.
const capCacheTTL = 5 * time.Minute

// capCache memoizes the last capability probe. Guarded by capCacheMu so concurrent
// heartbeat/register callers never race.
var (
	capCacheMu  sync.Mutex
	capCached   proto.Capabilities
	capCachedAt time.Time
	capCacheOK  bool
)

// cachedCapabilities returns the cached capability set, re-probing via
// detectCapabilities only when the cache is empty or older than capCacheTTL. The
// very first call (and the post-TTL refresh) does a real probe, so register's
// initial frame and the slow refresh both reflect ground truth; every heartbeat
// in between is served from cache. Context-aware: a fresh probe honors ctx.
func cachedCapabilities(ctx context.Context) proto.Capabilities {
	capCacheMu.Lock()
	if capCacheOK && time.Since(capCachedAt) < capCacheTTL {
		c := capCached
		capCacheMu.Unlock()
		return c
	}
	capCacheMu.Unlock()

	c := detectCapabilities(ctx)

	capCacheMu.Lock()
	capCached = c
	capCachedAt = time.Now()
	capCacheOK = true
	capCacheMu.Unlock()
	return c
}

// detectCapabilities probes for the claude + node binaries and their versions.
// It feeds the placement hard filter (D19), so it must reflect what the agent
// can ACTUALLY launch — including under the Windows Scheduled-Task service whose
// PATH differs from an interactive shell (claude.exe may not be on PATH there).
func detectCapabilities(ctx context.Context) proto.Capabilities {
	var c proto.Capabilities

	if path := resolveClaude(); path != "" {
		c.ClaudeInstalled = true
		c.ClaudeVersion = probeVersion(ctx, path, "--version")
		// Installed is not enough — claude must also be able to AUTH here (D22/F14).
		c.ClaudeAuthable = claudeAuthable()
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
	// Phase 4 integrations panel: Syncthing presence + liveness. Both are cheap and
	// cached with the rest of the capability set (5min).
	c.SyncthingInstalled = resolveSyncthing() != ""
	c.SyncthingRunning = syncthingRunning()
	return c
}

// resolveSyncthing finds the syncthing binary. PATH first, then OS-specific common
// install locations (Homebrew, /usr/*/bin, ~/.local/bin, Windows Programs).
func resolveSyncthing() string {
	name := "syncthing"
	if runtime.GOOS == "windows" {
		name = "syncthing.exe"
	}
	if path, err := exec.LookPath(name); err == nil {
		return path
	}
	for _, cand := range syncthingFallbackPaths() {
		if fi, err := os.Stat(cand); err == nil && !fi.IsDir() {
			return cand
		}
	}
	return ""
}

// syncthingFallbackPaths returns OS-specific common Syncthing install locations
// to check when PATH lookup fails (mirrors resolveClaude's fallback strategy).
func syncthingFallbackPaths() []string {
	home, _ := os.UserHomeDir()
	if runtime.GOOS == "windows" {
		if home == "" {
			return nil
		}
		return []string{
			filepath.Join(home, "AppData", "Local", "Programs", "Syncthing", "syncthing.exe"),
			filepath.Join(home, "AppData", "Local", "Microsoft", "WinGet", "Links", "syncthing.exe"),
		}
	}
	paths := []string{
		"/opt/homebrew/bin/syncthing",
		"/usr/local/bin/syncthing",
		"/usr/bin/syncthing",
	}
	if home != "" {
		paths = append(paths, filepath.Join(home, ".local", "bin", "syncthing"))
	}
	return paths
}

// syncthingRunning reports whether the local Syncthing GUI/API (127.0.0.1:8384)
// accepts a TCP connection within ~500ms. A successful dial means the daemon is
// up regardless of how it was started (Homebrew service, launchd, schtask, etc.).
func syncthingRunning() bool {
	conn, err := net.DialTimeout("tcp", "127.0.0.1:8384", 500*time.Millisecond)
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}

// claudeAuthable reports whether claude can complete its OAuth sign-in in THIS
// agent's process context (D22, F14). It is the can-actually-launch-claude signal
// that ClaudeInstalled alone is missing.
//
// On macOS, claude reads its OAuth credentials from the user's LOGIN KEYCHAIN. The
// keychain is only unlockable from a process that launchd started as a GUI
// LaunchAgent (the D22 reprovision). A process started by a generic process
// manager (pm2/nohup) — e.g. a hub-host agent — still shares the GUI audit session
// (so `launchctl managername` reports "Aqua" for BOTH, which is why managername
// does NOT discriminate, verified empirically), yet it is not a launchd-managed
// job and cannot reach the login keychain, so claude hangs on a blank auth prompt.
//
// The reliable discriminator is XPC_SERVICE_NAME: launchd sets it to the job label
// (e.g. "sh.lattice.agent") for the LaunchAgents it manages, while pm2/nohup
// children inherit the sentinel "0". Empirically: a LaunchAgent-managed agent
// reports "sh.lattice.agent"; a pm2-managed one reports "0". A root LaunchDaemon
// would have a label too but no user login keychain, so euid 0 is also excluded.
//
// On non-darwin, claude uses a file-based credential store with no GUI-session
// requirement, so install ⇒ authable (the per-OS LaunchAgent/Keychain gotcha is
// macOS-only; on hosts without claude this stays inert anyway).
func claudeAuthable() bool {
	if runtime.GOOS != "darwin" {
		return true
	}
	if os.Geteuid() == 0 {
		return false // a root LaunchDaemon has no per-user login keychain
	}
	svc := strings.TrimSpace(os.Getenv("XPC_SERVICE_NAME"))
	return svc != "" && svc != "0"
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
