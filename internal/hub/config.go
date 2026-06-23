package hub

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
)

// Config is the optional per-host configuration that de-personalizes a Lattice
// hub so anyone can run one. It is read once at startup from ~/.lattice/config.json.
//
// Precedence: config file → defaults. A field present in the file overrides its
// default; a missing/empty field falls back to the default. CLI flags override
// the resolved config, because the config values are wired in as the flag
// DEFAULTS (an explicitly-passed flag therefore wins over the file).
//
// There is no built-in personalization: with no config file every value takes a
// generic default (empty exclude list, project registry disabled), so a stock
// binary behaves vanilla. An operator who wants the optional integrations supplies
// a config.json written separately.
type Config struct {
	ProjectsRoot    string   `json:"projectsRoot"`
	MeshName        string   `json:"meshName"`
	Addr            string   `json:"addr"`
	ExcludedDevices []string `json:"excludedDevices"`

	// HubURL is the canonical, externally-reachable base URL of this hub, e.g.
	// "http://myhub.your-tailnet.ts.net:7400" (no trailing slash). When set, the
	// OPEN installer endpoints (/install.sh, /install.ps1) and the enroll one-
	// liners build their curl|sh URL from THIS value instead of the request's
	// Host header — so a spoofed Host can't redirect an enrolling box at an
	// attacker-controlled binary. Empty (the default) preserves the legacy
	// behavior of trusting r.Host, which is correct for a stock LAN/tailnet hub
	// reached directly by its real address. omitempty so stock configs stay clean.
	HubURL string `json:"hubUrl,omitempty"`

	// ProjectRegistry, when true AND ProjectRegistryPath is set, makes a newly
	// scaffolded project register itself into an external markdown registry file
	// (a row appended to a "## Project Registry" table). Disabled by default so a
	// stock hub never touches files outside its projects root.
	ProjectRegistry bool `json:"projectRegistry"`
	// ProjectRegistryPath is the markdown file whose "## Project Registry" table a
	// new project row is appended to. Empty ⇒ registration is disabled regardless
	// of ProjectRegistry.
	ProjectRegistryPath string `json:"projectRegistryPath,omitempty"`

	// PreviewPortMin/Max bound which agent loopback ports the preview proxy may
	// reach (SSRF containment — see previewproxy.go). Whoever installs Lattice can
	// widen/narrow this to match their dev servers; 0 on either field falls back to
	// the default dev-server range (3000-9999). omitempty so stock configs stay clean.
	PreviewPortMin int `json:"previewPortMin,omitempty"`
	PreviewPortMax int `json:"previewPortMax,omitempty"`

	// AdminPasswordHash is the bcrypt hash of the dashboard admin password,
	// collected by the first-run setup wizard (Phase 2). Empty until setup runs;
	// Phase 3 wires it into login. omitempty so legacy/unconfigured files stay clean.
	AdminPasswordHash string `json:"adminPasswordHash,omitempty"`

	// SetupComplete gates the first-run wizard. It is a *bool on purpose: an
	// ABSENT field (nil) means a legacy/hand-written config that predates the
	// wizard and must be treated as already-configured (no wizard). An explicit
	// false (written by `hub init`) means setup has not yet been finished.
	SetupComplete *bool `json:"setupComplete,omitempty"`
}

// configPath returns ~/.lattice/config.json, or "" if the home directory is
// unknown (treated as "no config" by LoadConfig).
func configPath() string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return ""
	}
	return filepath.Join(home, ".lattice", "config.json")
}

// configSource reports where the running config came from, for the startup log:
// the config.json path if it exists, else "defaults".
func configSource() string {
	path := configPath()
	if path == "" {
		return "defaults"
	}
	if _, err := os.Stat(path); err == nil {
		return path
	}
	return "defaults"
}

// defaultConfig is the generic, de-personalized baseline: the generic
// projects-root fallback, mesh name "lattice", the previous default listen
// address, no excluded devices, and the project registry disabled.
func defaultConfig() Config {
	return Config{
		ProjectsRoot:    defaultProjectsRoot(),
		MeshName:        "lattice",
		Addr:            ":7400",
		ExcludedDevices: nil,
		ProjectRegistry: false,
		PreviewPortMin:  defaultPreviewPortMin,
		PreviewPortMax:  defaultPreviewPortMax,
	}
}

// LoadConfig reads ~/.lattice/config.json and merges it over the defaults.
// A missing file is normal (every value defaults). A malformed file logs a
// warning and falls back to defaults rather than crashing. Individual
// missing/empty string fields also fall back to their default.
func LoadConfig() Config {
	cfg := defaultConfig()

	path := configPath()
	if path == "" {
		return cfg
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if !os.IsNotExist(err) {
			log.Printf("config: could not read %s: %v (using defaults)", path, err)
		}
		return cfg
	}

	var fromFile Config
	if err := json.Unmarshal(data, &fromFile); err != nil {
		log.Printf("config: malformed %s: %v (using defaults)", path, err)
		return defaultConfig()
	}

	if fromFile.ProjectsRoot != "" {
		cfg.ProjectsRoot = fromFile.ProjectsRoot
	}
	if fromFile.MeshName != "" {
		cfg.MeshName = fromFile.MeshName
	}
	if fromFile.Addr != "" {
		cfg.Addr = fromFile.Addr
	}
	if fromFile.ExcludedDevices != nil {
		cfg.ExcludedDevices = fromFile.ExcludedDevices
	}
	if fromFile.HubURL != "" {
		cfg.HubURL = fromFile.HubURL
	}
	cfg.ProjectRegistry = fromFile.ProjectRegistry
	if fromFile.ProjectRegistryPath != "" {
		cfg.ProjectRegistryPath = fromFile.ProjectRegistryPath
	}
	if fromFile.PreviewPortMin != 0 {
		cfg.PreviewPortMin = fromFile.PreviewPortMin
	}
	if fromFile.PreviewPortMax != 0 {
		cfg.PreviewPortMax = fromFile.PreviewPortMax
	}

	// Carry the setup fields through verbatim — do NOT default them. The hash is
	// copied only when present; the SetupComplete pointer is copied as-is so an
	// absent field stays nil (legacy = already-configured, see NeedsSetup).
	if fromFile.AdminPasswordHash != "" {
		cfg.AdminPasswordHash = fromFile.AdminPasswordHash
	}
	cfg.SetupComplete = fromFile.SetupComplete

	return cfg
}

// SaveConfig writes the full config to ~/.lattice/config.json atomically (write
// a temp file in the same dir, then os.Rename). It preserves every field of the
// struct it is handed, so callers must load-modify-save rather than constructing
// a partial Config (otherwise excludedDevices/projectRegistry would be lost).
func SaveConfig(cfg Config) error {
	path := configPath()
	if path == "" {
		return fmt.Errorf("config: home directory unknown")
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("config: mkdir %s: %w", dir, err)
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("config: marshal: %w", err)
	}
	data = append(data, '\n')

	tmp, err := os.CreateTemp(dir, ".config-*.json.tmp")
	if err != nil {
		return fmt.Errorf("config: create temp: %w", err)
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		_ = os.Remove(tmpName) // best-effort cleanup of the temp file on the error path
		return fmt.Errorf("config: write temp: %w", err)
	}
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		_ = os.Remove(tmpName) // best-effort cleanup of the temp file on the error path
		return fmt.Errorf("config: chmod temp: %w", err)
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName) // best-effort cleanup of the temp file on the error path
		return fmt.Errorf("config: close temp: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		_ = os.Remove(tmpName) // best-effort cleanup of the temp file on the error path
		return fmt.Errorf("config: rename into place: %w", err)
	}
	return nil
}

// NeedsSetup reports whether the first-run wizard should be shown. Only an
// EXPLICIT setupComplete:false (written by `hub init`) triggers it; an absent
// field (nil, legacy config) is treated as already-configured.
func NeedsSetup(cfg Config) bool {
	return cfg.SetupComplete != nil && !*cfg.SetupComplete
}

// configExists reports whether the config.json file is present on disk.
func configExists() bool {
	path := configPath()
	if path == "" {
		return false
	}
	_, err := os.Stat(path)
	return err == nil
}

// tokenFilePath returns ~/.lattice/.lattice-token, or "" if home is unknown.
func tokenFilePath() string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return ""
	}
	return filepath.Join(home, ".lattice", ".lattice-token")
}

// LoadPersistedToken returns the trimmed enrollment token from
// ~/.lattice/.lattice-token, or "" if the file is absent/unreadable.
func LoadPersistedToken() string {
	path := tokenFilePath()
	if path == "" {
		return ""
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

// PersistToken writes the enrollment token to ~/.lattice/.lattice-token so a
// hub keeps a stable token across restarts (and so the installer can read it).
func PersistToken(tok string) error {
	path := tokenFilePath()
	if path == "" {
		return fmt.Errorf("config: home directory unknown")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("config: mkdir for token: %w", err)
	}
	if err := os.WriteFile(path, []byte(tok+"\n"), 0o600); err != nil {
		return fmt.Errorf("config: write token: %w", err)
	}
	return nil
}

// boolPtr returns a pointer to b, for setting the *bool SetupComplete field.
func boolPtr(b bool) *bool { return &b }
