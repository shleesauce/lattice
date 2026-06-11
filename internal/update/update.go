// Package update implements `lattice update`: a role-agnostic self-updater.
//
// The same artifact runs as hub or agent (docs/DECISIONS.md D1), so updating is
// just "swap the binary in place" — no role logic. It pulls the matching asset
// from the GitHub Releases download base (the same base get.sh uses), verifies
// it against SHA256SUMS, and atomically replaces the currently-running binary.
// With --restart it best-effort restarts whichever Lattice service is installed.
package update

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

const defaultBase = "https://github.com/shleesauce/lattice/releases/latest/download"

// Options configures Apply, the programmatic self-updater shared by the CLI
// (`lattice update`) and the in-process callers (the hub's POST /api/update and
// the agent's TypeUpdate handler).
type Options struct {
	// Base is the download base URL. Empty ⇒ resolveBase ($LATTICE_DOWNLOAD_BASE
	// or the GitHub Releases latest base). The hub threads its own resolved base
	// down to every agent so the whole fleet pulls the SAME build (lockstep, D34).
	Base string
	// Insecure skips SHA256SUMS verification. NEVER set from the network-facing
	// callers; the only path that sets it is the CLI's explicit --insecure flag.
	Insecure bool
}

// Apply downloads the release asset matching this OS/arch, verifies its checksum
// (FAIL CLOSED unless Insecure), and atomically replaces the running binary. It
// does NOT restart — call Restart separately so callers can sequence the restart
// (e.g. the hub restarts itself only after every agent has swapped). The returned
// string is the resolved download base actually used, so the hub can hand the
// identical base to the agents and guarantee one fleet-wide build.
func Apply(ctx context.Context, opts Options) (string, error) {
	resolvedBase := strings.TrimRight(resolveBase(opts.Base), "/")
	asset := assetName()

	// HTTPS-pin the download channel. The asset AND its SHA256SUMS are fetched from
	// the same base, so a plain-http base means an in-transit attacker could serve a
	// tampered binary together with a matching checksum and verification proves
	// nothing ("checksum-from-same-origin"). HTTPS authenticates the origin and
	// prevents the swap. The GitHub default base is https; this only bites a
	// misconfigured/downgraded base. Exempt loopback (a same-box file server has no
	// in-transit surface) and the explicit --insecure path (the operator already
	// opted out of verification). LATTICE_DOWNLOAD_INSECURE=1 allows a plain-http
	// base for local mock-cascade testing over the tailnet.
	if !opts.Insecure {
		if err := requireSecureBase(resolvedBase); err != nil {
			return resolvedBase, err
		}
	}

	target, err := targetPath()
	if err != nil {
		return resolvedBase, fmt.Errorf("locate binary: %w", err)
	}
	dir := filepath.Dir(target)

	client := &http.Client{Timeout: 60 * time.Second}

	// Download the new binary into the SAME directory as the target so the final
	// rename stays on one filesystem and is atomic.
	tmp, err := os.CreateTemp(dir, ".lattice-update-*")
	if err != nil {
		return resolvedBase, fmt.Errorf("temp file: %w", err)
	}
	tmpPath := tmp.Name()
	cleanup := func() { _ = os.Remove(tmpPath) }

	assetURL := resolvedBase + "/" + asset
	sum, err := download(ctx, client, assetURL, tmp)
	_ = tmp.Close()
	if err != nil {
		cleanup()
		return resolvedBase, fmt.Errorf("download %s: %w", asset, err)
	}

	// Verify against SHA256SUMS — FAIL CLOSED. This is a self-updater that swaps the
	// running binary, so a missing/unreachable/incomplete sums file must ABORT, not
	// proceed: an attacker who can block (or strip the asset's line from) just the
	// sums file would otherwise defeat integrity entirely while still serving a
	// tampered binary. The only escape hatch is the explicit --insecure flag.
	want, err := fetchChecksum(ctx, client, resolvedBase+"/SHA256SUMS", asset)
	switch {
	case opts.Insecure:
		// caller explicitly opted out of verification (CLI --insecure only).
	case err != nil:
		cleanup()
		return resolvedBase, fmt.Errorf("SHA256SUMS unavailable (%w); aborting (binary NOT replaced)", err)
	case want == "":
		cleanup()
		return resolvedBase, fmt.Errorf("%s not listed in SHA256SUMS; aborting (binary NOT replaced)", asset)
	case !strings.EqualFold(want, sum):
		cleanup()
		return resolvedBase, fmt.Errorf("checksum mismatch for %s: expected %s, got %s (aborting, binary NOT replaced)", asset, want, sum)
	}

	if err := os.Chmod(tmpPath, 0o755); err != nil {
		cleanup()
		return resolvedBase, fmt.Errorf("chmod: %w", err)
	}

	if err := replace(tmpPath, target); err != nil {
		cleanup()
		return resolvedBase, fmt.Errorf("replace binary: %w", err)
	}

	return resolvedBase, nil
}

// Restart best-effort restarts whichever Lattice service (hub or agent) is
// installed on this box, applying the freshly-swapped binary. Returns the label
// restarted (or "" if none found). Exposed so the hub/agent update handlers can
// re-exec themselves after Apply.
func Restart() string { return restartService() }

// Run downloads the release asset matching this OS/arch, verifies its checksum,
// and atomically replaces the running binary. version is the current build's
// stamped main.Version (for the result message).
func Run(ctx context.Context, args []string, version string) error {
	fs := flag.NewFlagSet("update", flag.ContinueOnError)
	base := fs.String("base", "", "download base URL (default: $LATTICE_DOWNLOAD_BASE or GitHub Releases latest)")
	restart := fs.Bool("restart", false, "restart the installed Lattice service after updating")
	insecure := fs.Bool("insecure", false, "skip SHA256SUMS verification (DANGEROUS: installs an unverified binary)")
	if err := fs.Parse(args); err != nil {
		return err
	}

	if *insecure {
		fmt.Fprintln(os.Stderr, "lattice: warning: --insecure set; installing WITHOUT checksum verification")
	}
	fmt.Printf("lattice: downloading %s/%s\n", strings.TrimRight(resolveBase(*base), "/"), assetName())

	target, _ := targetPath()
	if _, err := Apply(ctx, Options{Base: *base, Insecure: *insecure}); err != nil {
		if *insecure {
			return fmt.Errorf("update: %w", err)
		}
		return fmt.Errorf("update: %w. Re-run with --insecure to override", err)
	}

	fmt.Printf("lattice: updated the binary at %s (was %s)\n", target, version)

	if *restart {
		if restarted := restartService(); restarted != "" {
			fmt.Printf("lattice: restarted %s\n", restarted)
		} else {
			fmt.Println("lattice: no installed Lattice service found to restart")
			fmt.Printf("lattice: restart manually with: %s\n", restartHint())
		}
		return nil
	}

	fmt.Println("lattice: restart the service to apply the update:")
	fmt.Printf("  %s\n", restartHint())
	return nil
}

// requireSecureBase rejects a non-https download base unless it is loopback or the
// operator explicitly opted out via LATTICE_DOWNLOAD_INSECURE=1. See Apply for why.
func requireSecureBase(base string) error {
	u, err := url.Parse(base)
	if err != nil {
		return fmt.Errorf("invalid download base %q: %w", base, err)
	}
	if u.Scheme == "https" {
		return nil
	}
	if isLoopbackHost(u.Hostname()) {
		return nil
	}
	if os.Getenv("LATTICE_DOWNLOAD_INSECURE") == "1" {
		fmt.Fprintf(os.Stderr, "lattice: warning: using INSECURE plain-http download base %q (LATTICE_DOWNLOAD_INSECURE=1)\n", base)
		return nil
	}
	return fmt.Errorf("refusing insecure download base %q: must be https (set LATTICE_DOWNLOAD_INSECURE=1 to allow plain http for local testing)", base)
}

// isLoopbackHost reports whether host is the loopback interface by name or IP.
func isLoopbackHost(host string) bool {
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func resolveBase(flagBase string) string {
	if flagBase != "" {
		return flagBase
	}
	if env := os.Getenv("LATTICE_DOWNLOAD_BASE"); env != "" {
		return env
	}
	return defaultBase
}

// assetName matches exactly what build.sh emits and get.sh downloads:
// lattice-<os>-<arch>[.exe].
func assetName() string {
	name := fmt.Sprintf("lattice-%s-%s", runtime.GOOS, runtime.GOARCH)
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	return name
}

// targetPath resolves the real binary to replace, following symlinks so we swap
// the actual file rather than a symlink pointing at it.
func targetPath() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	real, err := filepath.EvalSymlinks(exe)
	if err != nil {
		return "", err
	}
	return real, nil
}

// download streams url into w and returns the hex sha256 of the bytes written.
func download(ctx context.Context, client *http.Client, url string, w io.Writer) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("http %s", resp.Status)
	}
	h := sha256.New()
	if _, err := io.Copy(w, io.TeeReader(resp.Body, h)); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// fetchChecksum downloads a SHA256SUMS file (lines of "sha256<two spaces>name",
// like sha256sum/shasum -a 256 output) and returns the hash for asset, or empty
// string if the asset is not listed. A non-200 response is returned as an error
// so the caller can warn-and-proceed.
func fetchChecksum(ctx context.Context, client *http.Client, url, asset string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("http %s", resp.Status)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	for _, line := range strings.Split(string(body), "\n") {
		fields := strings.Fields(line)
		if len(fields) != 2 {
			continue
		}
		// The filename may be prefixed with '*' in binary-mode sums; strip it.
		name := strings.TrimPrefix(fields[1], "*")
		if name == asset {
			return fields[0], nil
		}
	}
	return "", nil
}

// replace atomically swaps tmp into target. On Unix, os.Rename over a running
// binary is safe (the process keeps its open inode). On Windows you can't rename
// over a running .exe, so move the old one aside first.
func replace(tmp, target string) error {
	if runtime.GOOS != "windows" {
		return os.Rename(tmp, target)
	}
	old := target + ".old"
	_ = os.Remove(old)
	if err := os.Rename(target, old); err != nil {
		return err
	}
	if err := os.Rename(tmp, target); err != nil {
		// Best-effort rollback so we don't leave the host without a binary.
		_ = os.Rename(old, target)
		return err
	}
	_ = os.Remove(old) // may be locked while running; ignore.
	return nil
}

// ServiceLabel returns the installed Lattice service label (hub or agent) WITHOUT
// restarting it, or "" if none is installed. The agent uses this to report its
// update outcome to the hub BEFORE it actually restarts: separating detect from
// restart lets the agent ack first, so the result frame reliably reaches the hub
// before the service teardown (kickstart -k / restart / schtasks) kills the
// connection and the hub's round-trip times out (the v0.1.5 cascade race).
func ServiceLabel() string { return detectService() }

// RestartByLabel restarts a specific, already-detected service label (from
// ServiceLabel). A "" label is a no-op. Best-effort: errors are returned, not
// fatal — a swapped binary still applies on the service's next start.
func RestartByLabel(label string) error { return restartByLabel(label) }

// RestartHint returns the exact manual restart command for this OS's hub service.
// Exposed so the hub's update handler can tell the operator how to finish an update
// when it runs under a service Lattice doesn't manage (pm2, a bare process) and
// therefore can't self-restart.
func RestartHint() string { return restartHint() }

// detectService probes for an installed Lattice service (hub or agent) and returns
// its label WITHOUT side effects, or "" if none is installed.
func detectService() string {
	switch runtime.GOOS {
	case "darwin":
		uid := os.Getuid()
		for _, label := range []string{"sh.lattice.hub", "sh.lattice.agent"} {
			if run("launchctl", "print", fmt.Sprintf("gui/%d/%s", uid, label)) == nil {
				return label
			}
		}
	case "linux":
		for _, unit := range []string{"lattice-hub", "lattice-agent"} {
			if run("systemctl", "--user", "status", unit) == nil {
				return unit
			}
		}
	case "windows":
		for _, task := range []string{"LatticeHub", "LatticeAgent"} {
			if run("schtasks", "/Query", "/TN", task) == nil {
				return task
			}
		}
	}
	return ""
}

// restartByLabel restarts the given service label for the current OS. "" is a no-op.
func restartByLabel(label string) error {
	if label == "" {
		return nil
	}
	switch runtime.GOOS {
	case "darwin":
		return run("launchctl", "kickstart", "-k", fmt.Sprintf("gui/%d/%s", os.Getuid(), label))
	case "linux":
		return run("systemctl", "--user", "restart", label)
	case "windows":
		_ = run("schtasks", "/End", "/TN", label)
		return run("schtasks", "/Run", "/TN", label)
	}
	return nil
}

// restartService detects the installed Lattice service and best-effort restarts it.
// Returns the label restarted, or "" if none was found / the restart failed.
// Failures are logged, not fatal.
func restartService() string {
	label := detectService()
	if label == "" {
		return ""
	}
	if err := restartByLabel(label); err != nil {
		fmt.Fprintf(os.Stderr, "lattice: warning: restart %s failed: %v\n", label, err)
		return ""
	}
	return label
}

// restartHint returns the exact restart command for the user's OS.
func restartHint() string {
	switch runtime.GOOS {
	case "darwin":
		return fmt.Sprintf("launchctl kickstart -k gui/%d/sh.lattice.hub", os.Getuid())
	case "linux":
		return "systemctl --user restart lattice-hub"
	case "windows":
		return "schtasks /End /TN LatticeHub && schtasks /Run /TN LatticeHub"
	default:
		return "restart the lattice service"
	}
}

func run(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Stdout = nil
	cmd.Stderr = nil
	return cmd.Run()
}
