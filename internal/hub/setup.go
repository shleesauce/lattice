package hub

import (
	"encoding/json"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"golang.org/x/crypto/bcrypt"
)

// bcryptCost is the work factor for hashing the admin password. 12 is a sensible
// modern default (~250ms/hash) and keeps logins responsive on the M-series boxes
// Lattice targets while staying well ahead of brute force.
const bcryptCost = 12

// maxMeshNameLen bounds the mesh name accepted by the setup wizard.
const maxMeshNameLen = 40

// ProjectsRoot returns the configured workspace root under the cfg lock. The
// wizard can rewrite it at runtime, so the 4 read sites go through this rather
// than touching h.projectsRoot directly.
func (h *Hub) ProjectsRoot() string {
	h.cfgMu.RLock()
	defer h.cfgMu.RUnlock()
	return h.projectsRoot
}

// needsSetup reports whether the first-run wizard still has to run.
func (h *Hub) needsSetup() bool {
	h.cfgMu.RLock()
	defer h.cfgMu.RUnlock()
	return !h.setupComplete
}

// MeshName returns the configured mesh name under the cfg lock.
func (h *Hub) MeshName() string {
	h.cfgMu.RLock()
	defer h.cfgMu.RUnlock()
	return h.meshName
}

// expandPath resolves a user-supplied path: expand a leading ~ / ~/ to the home
// directory, Clean, and make it absolute. It never touches the filesystem.
func expandPath(p string) (string, error) {
	p = strings.TrimSpace(p)
	if p == "~" || strings.HasPrefix(p, "~/") {
		home, err := os.UserHomeDir()
		if err != nil || home == "" {
			return "", err
		}
		if p == "~" {
			p = home
		} else {
			p = filepath.Join(home, p[2:])
		}
	}
	p = filepath.Clean(p)
	abs, err := filepath.Abs(p)
	if err != nil {
		return "", err
	}
	return abs, nil
}

// handleSetupStatus answers GET /api/setup/status (unauthenticated). When setup
// is already complete it reveals nothing but {"needsSetup":false}; otherwise it
// returns the prefill the wizard renders (current/suggested values + hostname).
func (h *Hub) handleSetupStatus(w http.ResponseWriter, r *http.Request) {
	if !h.needsSetup() {
		writeJSON(w, http.StatusOK, map[string]any{"needsSetup": false})
		return
	}
	hostname, _ := os.Hostname()
	writeJSON(w, http.StatusOK, map[string]any{
		"needsSetup":    true,
		"meshName":      h.MeshName(),
		"projectsRoot":  h.ProjectsRoot(),
		"hostname":      hostname,
		"suggestedRoot": defaultProjectsRoot(),
		// tokenRequired tells the wizard whether finishing setup needs the hub token.
		// A loopback caller (operator on the hub box) may finish without it; a remote
		// tailnet browser must paste the token printed by the installer (see
		// setupAllowed). Computed per-request from THIS connection's origin.
		"tokenRequired": !requestIsLoopback(r),
	})
}

// setupAllowed reports whether r may finish the first-run setup wizard. Before an
// admin password exists, the hub is reachable by every tailnet peer with no
// credential, so an open POST /api/setup is an unauthenticated takeover window: the
// first peer to find the box could claim admin and lock out the real operator. We
// require either:
//   - a loopback connection (the operator is on the hub box itself), or
//   - the on-disk master token as a Bearer credential (printed by the installer; the
//     operator pastes it into the wizard).
//
// check-root stays open (it only validates a path), so the wizard's live feedback
// works for everyone; only the actual admin-creating POST is gated.
func (h *Hub) setupAllowed(r *http.Request) bool {
	return requestIsLoopback(r) || h.bearerIsMasterToken(r)
}

// handleSetupCheckRoot answers POST /api/setup/check-root (unauthenticated): it
// validates a candidate projects-root path and reports whether it exists, is a
// directory, or would be created — without mutating anything. Gated 409 once
// setup is complete so it cannot be probed on a configured hub.
func (h *Hub) handleSetupCheckRoot(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !h.needsSetup() {
		writeJSON(w, http.StatusConflict, map[string]any{"error": "hub already configured"})
		return
	}
	var body struct {
		Path string `json:"path"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid json body"})
		return
	}

	resolved, err := expandPath(body.Path)
	if err != nil || resolved == "" {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": "could not resolve path"})
		return
	}

	info, statErr := os.Stat(resolved)
	switch {
	case statErr == nil && info.IsDir():
		writeJSON(w, http.StatusOK, map[string]any{
			"ok": true, "resolved": resolved, "exists": true, "willCreate": false,
		})
	case statErr == nil:
		writeJSON(w, http.StatusOK, map[string]any{
			"ok": false, "error": "path is a file, not a directory",
		})
	default:
		// Does not exist yet — creatable only if the parent directory is there.
		if parent, perr := os.Stat(filepath.Dir(resolved)); perr == nil && parent.IsDir() {
			writeJSON(w, http.StatusOK, map[string]any{
				"ok": true, "resolved": resolved, "exists": false, "willCreate": true,
			})
		} else {
			writeJSON(w, http.StatusOK, map[string]any{
				"ok": false, "error": "parent directory does not exist",
			})
		}
	}
}

// handleSetup answers POST /api/setup (unauthenticated): the first-run wizard
// submits the admin password, mesh name, and projects root. It validates, hashes
// the password (bcrypt), persists the full config, and flips the hub out of
// setup mode. Gated 409 once complete. This phase only COLLECTS + STORES the
// admin password — no route is auth-gated here; Phase 3 wires login.
func (h *Hub) handleSetup(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !h.needsSetup() {
		writeJSON(w, http.StatusConflict, map[string]any{"error": "hub already configured"})
		return
	}
	// Block the unauthenticated-takeover window: a remote peer must present the hub
	// token; only a loopback operator may finish setup credential-free (see setupAllowed).
	if !h.setupAllowed(r) {
		w.Header().Set("WWW-Authenticate", "Bearer")
		writeJSON(w, http.StatusUnauthorized, map[string]any{"error": "setup requires the hub token (or a connection from the hub machine)"})
		return
	}
	var body struct {
		AdminPassword string `json:"adminPassword"`
		MeshName      string `json:"meshName"`
		ProjectsRoot  string `json:"projectsRoot"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid json body"})
		return
	}

	if utf8.RuneCountInString(body.AdminPassword) < 8 {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "password must be at least 8 characters"})
		return
	}
	meshName := strings.TrimSpace(body.MeshName)
	if n := utf8.RuneCountInString(meshName); n < 1 || n > maxMeshNameLen {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "mesh name must be 1-40 characters"})
		return
	}

	resolved, err := expandPath(body.ProjectsRoot)
	if err != nil || resolved == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "projects root could not be resolved"})
		return
	}
	if info, statErr := os.Stat(resolved); statErr == nil {
		if !info.IsDir() {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": "projects root is a file, not a directory"})
			return
		}
	} else if err := os.MkdirAll(resolved, 0o755); err != nil {
		log.Printf("setup: mkdir projects root failed: %v", err)
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "projects root could not be created"})
		return
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(body.AdminPassword), bcryptCost)
	if err != nil {
		log.Printf("setup: bcrypt failed: %v", err)
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "could not secure password"})
		return
	}

	// Load-modify-save so excludedDevices/projectRegistry etc. are preserved.
	cfg := LoadConfig()
	cfg.MeshName = meshName
	cfg.ProjectsRoot = resolved
	cfg.AdminPasswordHash = string(hash)
	cfg.SetupComplete = boolPtr(true)
	if err := SaveConfig(cfg); err != nil {
		log.Printf("setup: save config failed: %v", err)
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "could not save configuration"})
		return
	}

	h.cfgMu.Lock()
	h.meshName = meshName
	h.projectsRoot = resolved
	h.adminPasswordHash = string(hash)
	h.setupComplete = true
	h.cfgMu.Unlock()

	// Auto-login (Phase 3): the wizard just set the admin password, so mint a
	// session + cookie before responding. The dashboard reload that follows the
	// wizard then lands already authenticated instead of bouncing to a login.
	setSessionCookie(w, h.sessions.create())

	log.Printf("setup: first-run wizard complete (mesh=%s, projects=%s)", meshName, resolved)
	writeJSON(w, http.StatusOK, map[string]any{
		"ok": true, "meshName": meshName, "projectsRoot": resolved,
	})
}
