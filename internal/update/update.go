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
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

const defaultBase = "https://github.com/shleesauce/lattice/releases/latest/download"

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

	resolvedBase := strings.TrimRight(resolveBase(*base), "/")
	asset := assetName()

	target, err := targetPath()
	if err != nil {
		return fmt.Errorf("update: locate binary: %w", err)
	}
	dir := filepath.Dir(target)

	client := &http.Client{Timeout: 60 * time.Second}

	// Download the new binary into the SAME directory as the target so the final
	// rename stays on one filesystem and is atomic.
	tmp, err := os.CreateTemp(dir, ".lattice-update-*")
	if err != nil {
		return fmt.Errorf("update: temp file: %w", err)
	}
	tmpPath := tmp.Name()
	cleanup := func() { _ = os.Remove(tmpPath) }

	assetURL := resolvedBase + "/" + asset
	fmt.Printf("lattice: downloading %s\n", assetURL)
	sum, err := download(ctx, client, assetURL, tmp)
	_ = tmp.Close()
	if err != nil {
		cleanup()
		return fmt.Errorf("update: download %s: %w", asset, err)
	}

	// Verify against SHA256SUMS — FAIL CLOSED. This is a self-updater that swaps the
	// running binary, so a missing/unreachable/incomplete sums file must ABORT, not
	// proceed: an attacker who can block (or strip the asset's line from) just the
	// sums file would otherwise defeat integrity entirely while still serving a
	// tampered binary. The only escape hatch is the explicit --insecure flag.
	want, err := fetchChecksum(ctx, client, resolvedBase+"/SHA256SUMS", asset)
	switch {
	case *insecure:
		fmt.Fprintln(os.Stderr, "lattice: warning: --insecure set; installing WITHOUT checksum verification")
	case err != nil:
		cleanup()
		return fmt.Errorf("update: SHA256SUMS unavailable (%w); aborting (binary NOT replaced). Re-run with --insecure to override", err)
	case want == "":
		cleanup()
		return fmt.Errorf("update: %s not listed in SHA256SUMS; aborting (binary NOT replaced). Re-run with --insecure to override", asset)
	case !strings.EqualFold(want, sum):
		cleanup()
		return fmt.Errorf("update: checksum mismatch for %s: expected %s, got %s (aborting, binary NOT replaced)", asset, want, sum)
	default:
		fmt.Printf("lattice: checksum verified (%s)\n", sum)
	}

	if err := os.Chmod(tmpPath, 0o755); err != nil {
		cleanup()
		return fmt.Errorf("update: chmod: %w", err)
	}

	if err := replace(tmpPath, target); err != nil {
		cleanup()
		return fmt.Errorf("update: replace binary: %w", err)
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

// restartService probes for an installed Lattice service (hub or agent) and
// best-effort restarts it. Returns the service label restarted, or "" if none
// was found. Failures are logged, not fatal.
func restartService() string {
	switch runtime.GOOS {
	case "darwin":
		uid := os.Getuid()
		for _, label := range []string{"sh.lattice.hub", "sh.lattice.agent"} {
			gui := fmt.Sprintf("gui/%d/%s", uid, label)
			if run("launchctl", "print", gui) != nil {
				continue // not installed
			}
			if err := run("launchctl", "kickstart", "-k", gui); err != nil {
				fmt.Fprintf(os.Stderr, "lattice: warning: restart %s failed: %v\n", label, err)
				continue
			}
			return label
		}
	case "linux":
		for _, unit := range []string{"lattice-hub", "lattice-agent"} {
			if run("systemctl", "--user", "status", unit) != nil {
				continue // not installed
			}
			if err := run("systemctl", "--user", "restart", unit); err != nil {
				fmt.Fprintf(os.Stderr, "lattice: warning: restart %s failed: %v\n", unit, err)
				continue
			}
			return unit
		}
	case "windows":
		for _, task := range []string{"LatticeHub", "LatticeAgent"} {
			if run("schtasks", "/Query", "/TN", task) != nil {
				continue // not installed
			}
			_ = run("schtasks", "/End", "/TN", task)
			if err := run("schtasks", "/Run", "/TN", task); err != nil {
				fmt.Fprintf(os.Stderr, "lattice: warning: restart %s failed: %v\n", task, err)
				continue
			}
			return task
		}
	}
	return ""
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
