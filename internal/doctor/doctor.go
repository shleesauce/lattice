// Package doctor implements `lattice doctor`: a role-agnostic health check that
// reports the machine, the Lattice config, whether a hub is reachable, and which
// capabilities + integrations are present. It is read-only and never fails the
// process on warnings — it's a diagnostic snapshot for setup + debugging, in the
// spirit of `brew doctor` / `tailscale status`.
package doctor

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/shleesauce/lattice/internal/hub"
)

// status is a check outcome. ok = healthy, warn = works-but-worth-knowing,
// fail = broken/unsupported, info = neutral (not a problem).
type status string

const (
	statusOK   status = "ok"
	statusWarn status = "warn"
	statusFail status = "fail"
	statusInfo status = "info"
)

// check is one diagnostic line.
type check struct {
	Group  string `json:"group"`
	Name   string `json:"name"`
	Status status `json:"status"`
	Detail string `json:"detail"`
}

// report is the full doctor result, marshalled in --json mode.
type report struct {
	Version string  `json:"version"`
	OS      string  `json:"os"`
	Arch    string  `json:"arch"`
	Checks  []check `json:"checks"`
	Summary struct {
		OK   int `json:"ok"`
		Warn int `json:"warn"`
		Fail int `json:"fail"`
	} `json:"summary"`
}

// probeTimeout bounds each external probe (version commands, tailscale status,
// the hub health request, the syncthing dial) so a wedged tool can't hang doctor.
const probeTimeout = 2 * time.Second

// Run executes the diagnostics and prints a report. args may contain --json.
func Run(ctx context.Context, args []string, version string) error {
	jsonOut := false
	for _, a := range args {
		if a == "--json" {
			jsonOut = true
		}
	}

	var checks []check
	add := func(group, name string, st status, detail string) {
		checks = append(checks, check{Group: group, Name: name, Status: st, Detail: detail})
	}

	// ── SYSTEM ──
	host, _ := os.Hostname()
	if supportedTarget() {
		add("system", "platform", statusOK, fmt.Sprintf("%s/%s (supported)", runtime.GOOS, runtime.GOARCH))
	} else {
		add("system", "platform", statusWarn, fmt.Sprintf("%s/%s (no published build — build from source)", runtime.GOOS, runtime.GOARCH))
	}
	add("system", "version", statusInfo, fmt.Sprintf("lattice %s", version))
	if host != "" {
		add("system", "hostname", statusInfo, host)
	}

	// ── CONFIG (~/.lattice) ──
	cfg := hub.LoadConfig()
	if configFileExists() {
		add("config", "config.json", statusOK, fmt.Sprintf("mesh %q, listen %s", cfg.MeshName, cfg.Addr))
	} else {
		add("config", "config.json", statusWarn, "not initialized — run the installer or `lattice hub init`")
	}
	if cfg.ProjectsRoot == "" {
		add("config", "projects root", statusWarn, "unset")
	} else if dirExists(cfg.ProjectsRoot) {
		add("config", "projects root", statusOK, cfg.ProjectsRoot+" (exists)")
	} else {
		add("config", "projects root", statusWarn, cfg.ProjectsRoot+" (missing — will be created on first use)")
	}
	if fileExists(tokenPath()) {
		add("config", "enrollment token", statusOK, "present (~/.lattice/.lattice-token)")
	} else {
		add("config", "enrollment token", statusWarn, "not found — minted on first hub start")
	}
	if cfg.AdminPasswordHash != "" {
		add("config", "auth", statusOK, "ON (admin password set)")
	} else {
		add("config", "auth", statusWarn, "OFF — dashboard is open; set with `lattice hub set-password`")
	}
	if hub.NeedsSetup(cfg) {
		add("config", "first-run setup", statusWarn, "pending — finish the wizard in the dashboard")
	} else {
		add("config", "first-run setup", statusOK, "complete")
	}

	// ── HUB (is one reachable from here?) ──
	if v, n, ok := hubHealth(cfg.Addr); ok {
		add("hub", "reachable", statusOK, fmt.Sprintf("responding on %s — version %s, %d agent(s)", localURL(cfg.Addr), v, n))
	} else {
		add("hub", "reachable", statusInfo, fmt.Sprintf("no hub on %s (fine if this is an agent-only box, or the hub isn't started)", localURL(cfg.Addr)))
	}

	// ── CAPABILITIES (what this machine can run) ──
	if path, ok := lookExec("claude"); ok {
		add("capabilities", "claude", statusOK, fmt.Sprintf("installed — %s", versionOf(ctx, path, "--version")))
	} else {
		add("capabilities", "claude", statusInfo, "not found — Claude sessions can't run on this machine")
	}
	if path, ok := lookExec("node"); ok {
		add("capabilities", "node", statusOK, "installed — "+versionOf(ctx, path, "--version"))
	} else {
		add("capabilities", "node", statusInfo, "not found")
	}
	if resolveCodeServer() != "" {
		add("capabilities", "code-server", statusOK, "installed — embedded VS Code editor available")
	} else {
		add("capabilities", "code-server", statusInfo, "not found — editor sessions unavailable here")
	}
	if _, ok := lookExec("git"); ok {
		add("capabilities", "git", statusOK, "installed")
	} else {
		add("capabilities", "git", statusWarn, "not found")
	}

	// ── INTEGRATIONS (detect + guide — the encryption/sync layer) ──
	if path, ok := lookExec("tailscale"); ok {
		if tailscaleUp(ctx, path) {
			add("integrations", "tailscale", statusOK, "installed + connected (your encryption layer)")
		} else {
			add("integrations", "tailscale", statusWarn, "installed but not connected — run `tailscale up`")
		}
	} else {
		add("integrations", "tailscale", statusWarn, "not installed — the recommended encryption layer (https://tailscale.com/download)")
	}
	stInstalled := resolveSyncthing() != ""
	stRunning := dialable("127.0.0.1:8384")
	switch {
	case stInstalled && stRunning:
		add("integrations", "syncthing", statusOK, "installed + running (GUI on 127.0.0.1:8384)")
	case stInstalled:
		add("integrations", "syncthing", statusWarn, "installed but not running")
	default:
		add("integrations", "syncthing", statusInfo, "not detected — optional, for syncing folders across the mesh (https://syncthing.net)")
	}
	if _, ok := lookExec("ssh"); ok {
		add("integrations", "ssh", statusOK, "client present")
	} else {
		add("integrations", "ssh", statusInfo, "no ssh client found")
	}

	// ── output ──
	if jsonOut {
		return emitJSON(version, checks)
	}
	emitText(checks)
	return nil
}

// ── output helpers ──

func emitText(checks []check) {
	tag := map[status]string{statusOK: "  ok ", statusWarn: " warn", statusFail: " FAIL", statusInfo: "  -  "}
	var ok, warn, fail int
	lastGroup := ""
	fmt.Println("lattice doctor")
	for _, c := range checks {
		if c.Group != lastGroup {
			fmt.Printf("\n%s\n", strings.ToUpper(c.Group))
			lastGroup = c.Group
		}
		fmt.Printf("  [%s] %-18s %s\n", tag[c.Status], c.Name, c.Detail)
		switch c.Status {
		case statusOK:
			ok++
		case statusWarn:
			warn++
		case statusFail:
			fail++
		}
	}
	fmt.Printf("\nsummary: %d ok, %d warning(s), %d problem(s)\n", ok, warn, fail)
	switch {
	case fail > 0:
		fmt.Println("→ some checks failed — see FAIL lines above.")
	case warn > 0:
		fmt.Println("→ healthy; the warnings above are worth a look but not blocking.")
	default:
		fmt.Println("→ all good.")
	}
}

func emitJSON(version string, checks []check) error {
	r := report{Version: version, OS: runtime.GOOS, Arch: runtime.GOARCH, Checks: checks}
	for _, c := range checks {
		switch c.Status {
		case statusOK:
			r.Summary.OK++
		case statusWarn:
			r.Summary.Warn++
		case statusFail:
			r.Summary.Fail++
		}
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(r)
}

// ── probes ──

// supportedTarget reports whether a prebuilt binary is published for this OS/arch.
func supportedTarget() bool {
	switch runtime.GOOS + "/" + runtime.GOARCH {
	case "darwin/arm64", "darwin/amd64", "windows/amd64", "linux/amd64", "linux/arm64":
		return true
	}
	return false
}

func latticeDir() string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return ".lattice"
	}
	return filepath.Join(home, ".lattice")
}

func configFileExists() bool { return fileExists(filepath.Join(latticeDir(), "config.json")) }
func tokenPath() string      { return filepath.Join(latticeDir(), ".lattice-token") }

func fileExists(p string) bool {
	fi, err := os.Stat(p)
	return err == nil && !fi.IsDir()
}

func dirExists(p string) bool {
	fi, err := os.Stat(p)
	return err == nil && fi.IsDir()
}

// localURL renders the loopback URL for the configured listen addr (":7400" →
// http://127.0.0.1:7400).
func localURL(addr string) string {
	return "http://127.0.0.1:" + portOf(addr)
}

// portOf extracts the port from a listen addr like ":7400" or "0.0.0.0:7400".
func portOf(addr string) string {
	if addr == "" {
		return "7400"
	}
	if _, port, err := net.SplitHostPort(addr); err == nil && port != "" {
		return port
	}
	return strings.TrimPrefix(addr, ":")
}

// hubHealth GETs /api/health on the loopback addr and returns the hub version +
// agent count if one answers.
func hubHealth(addr string) (version string, agents int, ok bool) {
	client := &http.Client{Timeout: probeTimeout}
	resp, err := client.Get(localURL(addr) + "/api/health")
	if err != nil {
		return "", 0, false
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", 0, false
	}
	var body struct {
		Version string `json:"version"`
		Agents  int    `json:"agents"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return "", 0, false
	}
	return body.Version, body.Agents, true
}

// lookExec resolves a binary on PATH (appending .exe on Windows).
func lookExec(name string) (string, bool) {
	if runtime.GOOS == "windows" && !strings.HasSuffix(name, ".exe") {
		name += ".exe"
	}
	p, err := exec.LookPath(name)
	return p, err == nil
}

// versionOf runs `<bin> <flag>` and returns the trimmed first line, or "installed".
func versionOf(ctx context.Context, bin, flag string) string {
	pctx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()
	out, err := exec.CommandContext(pctx, bin, flag).Output()
	if err != nil {
		return "installed"
	}
	line := strings.TrimSpace(string(out))
	if i := strings.IndexByte(line, '\n'); i >= 0 {
		line = strings.TrimSpace(line[:i])
	}
	if line == "" {
		return "installed"
	}
	return line
}

// tailscaleUp reports whether `tailscale status` succeeds (node is connected).
func tailscaleUp(ctx context.Context, bin string) bool {
	pctx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()
	return exec.CommandContext(pctx, bin, "status").Run() == nil
}

// dialable reports whether a TCP connection to addr succeeds quickly.
func dialable(addr string) bool {
	conn, err := net.DialTimeout("tcp", addr, 500*time.Millisecond)
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}

// resolveCodeServer locates a code-server binary (PATH + common install dirs).
func resolveCodeServer() string {
	if p, err := exec.LookPath("code-server"); err == nil {
		return p
	}
	home, _ := os.UserHomeDir()
	cands := []string{"/opt/homebrew/bin/code-server", "/usr/local/bin/code-server"}
	if home != "" {
		cands = append(cands, filepath.Join(home, ".local", "bin", "code-server"))
	}
	for _, c := range cands {
		if fileExists(c) {
			return c
		}
	}
	return ""
}

// resolveSyncthing locates a syncthing binary (PATH + common install dirs).
func resolveSyncthing() string {
	name := "syncthing"
	if runtime.GOOS == "windows" {
		name = "syncthing.exe"
	}
	if p, err := exec.LookPath(name); err == nil {
		return p
	}
	home, _ := os.UserHomeDir()
	cands := []string{"/opt/homebrew/bin/syncthing", "/usr/local/bin/syncthing", "/usr/bin/syncthing"}
	if home != "" {
		cands = append(cands, filepath.Join(home, ".local", "bin", name))
	}
	for _, c := range cands {
		if fileExists(c) {
			return c
		}
	}
	return ""
}
