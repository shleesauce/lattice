package hub

import (
	"bytes"
	"embed"
	"net/http"
	"os"
	"path"
	"regexp"
	"strings"
	"text/template"
)

// agentDownloadBaseDefault is where /dl/ redirects when a requested agent binary
// isn't in the local dist dir. A hub installed by get.sh has only its OWN binary
// (no cross-compiled set) and its service runs with no populated dist/, so without
// this fallback every enrolling machine's binary download 404s and no second
// machine can ever join. Redirecting to the public release also lets a hub of one
// OS enroll an agent of another (a Mac hub serving a Windows agent) with nothing
// bundled. Mirrors the base used by get.sh and `lattice update`.
const agentDownloadBaseDefault = "https://github.com/shleesauce/lattice/releases/latest/download"

// agentDownloadBase honors LATTICE_DOWNLOAD_BASE (for a custom/air-gapped mirror),
// matching get.sh and the self-updater, else the public release.
func agentDownloadBase() string {
	if env := os.Getenv("LATTICE_DOWNLOAD_BASE"); env != "" {
		return strings.TrimRight(env, "/")
	}
	return agentDownloadBaseDefault
}

// installTmplFS embeds the canonical installer templates. They are rendered per
// request with the hub URL derived from the inbound request, so the same binary
// serves correct installers regardless of how it is reached (LAN IP, tailnet
// hostname, localhost). The token is never baked in — the operator supplies it
// at run time so the served script is safe to expose.
//
//go:embed installtmpl/*.tmpl
var installTmplFS embed.FS

// installTmpls is parsed once at startup; a parse failure is a build-time bug.
var installTmpls = template.Must(template.ParseFS(installTmplFS, "installtmpl/*.tmpl"))

// safeBinaryName matches the only filenames /dl/{name} will serve. It pins the
// shape of our cross-compiled artifacts (lattice-<os>-<arch>[.exe]) and rejects
// anything with a path separator, dot-dot, or unexpected characters, so there
// is no path-traversal surface even before http.ServeFile's own cleaning.
var safeBinaryName = regexp.MustCompile(`^lattice-[a-z0-9]+-[a-z0-9]+(\.exe)?$`)

// installData is the template context for both installers.
type installData struct {
	// HubURL is the scheme+host the agent and downloads should target, e.g.
	// http://myhub.your-tailnet.ts.net:7400 (no trailing slash).
	HubURL string
	// HubHostPort is HubURL stripped of its scheme, e.g.
	// myhub.your-tailnet.ts.net:7400 — the value passed to `agent --hub`.
	HubHostPort string
}

// hubURLFromRequest reconstructs the externally-reachable hub URL from the
// request. We only serve plain http for self-hosted meshes; if the hub is
// fronted by TLS the operator can override via X-Forwarded-Proto.
//
// NOTE: both the scheme (X-Forwarded-Proto) and the host (r.Host) here are
// attacker-controllable on the OPEN installer endpoints. Prefer
// (*Hub).canonicalHubURL, which uses the operator-configured HubURL when set and
// only falls back to this request-derived value when nothing is configured.
func hubURLFromRequest(r *http.Request) string {
	scheme := "http"
	if proto := r.Header.Get("X-Forwarded-Proto"); proto == "https" {
		scheme = "https"
	} else if r.TLS != nil {
		scheme = "https"
	}
	return scheme + "://" + r.Host
}

// canonicalHubURL returns the base URL (no trailing slash) the installer scripts
// and enroll one-liners should target. It prefers the operator-configured
// canonical URL (Config.HubURL), so the curl|sh base can't be steered by a
// spoofed Host/X-Forwarded-Proto on the unauthenticated installer endpoints.
// When no canonical URL is configured it falls back to the request-derived value
// — correct for a stock LAN/tailnet hub reached directly by its real address.
func (h *Hub) canonicalHubURL(r *http.Request) string {
	if h.hubURL != "" {
		return h.hubURL
	}
	return hubURLFromRequest(r)
}

// hubHostPort strips the scheme from a base URL, yielding host:port for the
// `agent --hub` flag. It tolerates a missing scheme (returns the input as-is).
func hubHostPort(url string) string {
	if i := strings.Index(url, "://"); i >= 0 {
		return url[i+3:]
	}
	return url
}

// handleDownloadBinary serves a cross-compiled agent binary from the dist dir.
// The {name} is validated against safeBinaryName before touching the filesystem.
func (h *Hub) handleDownloadBinary(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	name := path.Base(r.URL.Path) // collapses any /dl/.. attempts to a basename
	if !safeBinaryName.MatchString(name) {
		http.NotFound(w, r)
		return
	}
	// Serve from the local dist dir when the binary is present (a dev/fleet hub
	// built by build.sh). When it's absent — the common case for a get.sh-installed
	// hub, which has no populated dist/ — redirect to the public release so the
	// enrolling machine fetches the official binary for ITS os/arch. The agent
	// installer's curl -fsSL / irm follow the redirect transparently. name is
	// already validated by safeBinaryName, so this is not an open redirect.
	local := path.Join(h.distDir, name)
	if _, err := os.Stat(local); err == nil {
		w.Header().Set("Content-Type", "application/octet-stream")
		http.ServeFile(w, r, local)
		return
	}
	http.Redirect(w, r, agentDownloadBase()+"/"+name, http.StatusFound)
}

// handleInstallSh renders the POSIX sh installer with the hub URL baked in.
func (h *Hub) handleInstallSh(w http.ResponseWriter, r *http.Request) {
	h.renderInstaller(w, r, "install.sh.tmpl", "text/x-shellscript; charset=utf-8")
}

// handleInstallPs1 renders the PowerShell installer with the hub URL baked in.
func (h *Hub) handleInstallPs1(w http.ResponseWriter, r *http.Request) {
	h.renderInstaller(w, r, "install.ps1.tmpl", "text/plain; charset=utf-8")
}

func (h *Hub) renderInstaller(w http.ResponseWriter, r *http.Request, name, contentType string) {
	url := h.canonicalHubURL(r)
	data := installData{HubURL: url, HubHostPort: hubHostPort(url)}
	var buf bytes.Buffer
	if err := installTmpls.ExecuteTemplate(&buf, name, data); err != nil {
		http.Error(w, "installer render failed", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", contentType)
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(buf.Bytes())
}

// enrollOneLiners builds the copy-paste install commands for a given hub URL and
// enrollment token. Shared by handleEnroll (master token) and the per-machine
// token mint endpoint (enrolltokens.go) so the two surfaces never drift.
//
// Both forms pass the token through the LATTICE_TOKEN environment variable rather
// than on the command line, so the credential never lands in argv (where it would
// be visible to anyone running `ps`) on the enrolling box. The installer scripts
// read LATTICE_TOKEN and the agent honours it too (see agent.parseFlags), so the
// secret stays in the environment end to end.
func enrollOneLiners(url, token string) (unix, windows string) {
	unix = "curl -fsSL " + url + "/install.sh | LATTICE_TOKEN='" + token + "' sh"
	windows = "$env:LATTICE_TOKEN='" + token + "'; irm " + url + "/install.ps1 | iex"
	return unix, windows
}

// tailscaleSetupOneLiners returns the official, copy-paste Tailscale install +
// `up` commands for a machine that isn't on the tailnet yet. The dashboard shows
// these as the "Step 1 (skip if already on Tailscale)" prerequisite alongside the
// enroll one-liner, so a new box can be brought onto the mesh in one place.
//
// Lattice intentionally does NOT run these for the user: Tailscale needs root to
// install and an interactive browser login for `up` (its own account), so this is
// a guided copy-paste the user runs in their own terminal — where sudo and the
// login actually work — not a privileged action the hub performs on their behalf.
// These commands are idempotent: re-running on an already-connected box is a
// near no-op, so it's safe to paste even if Tailscale is already set up.
func tailscaleSetupOneLiners() (unix, windows string) {
	unix = "curl -fsSL https://tailscale.com/install.sh | sh && sudo tailscale up"
	windows = "winget install -e --id tailscale.tailscale --source winget; tailscale up"
	return unix, windows
}

// handleEnroll returns the copy-paste enrollment one-liners for the dashboard
// onboarding panel. The current hub token is included so the operator can hand
// it straight to a new machine.
//
// This endpoint HANDS OUT the master token, so its route wrapper (requireAuth) is
// not enough: when no admin password is configured requireAuth is a pass-through,
// which would expose the master token to anyone who can reach the port. The
// requireAuthOrToken gate below closes that — on a passwordless hub the caller must
// present the master token as a Bearer credential to read it back (no new secret is
// disclosed to an unauthenticated caller). On a password-protected hub it is the
// normal admin gate (session cookie or master-token Bearer).
func (h *Hub) handleEnroll(w http.ResponseWriter, r *http.Request) {
	h.requireAuthOrToken(h.handleEnrollInner)(w, r)
}

func (h *Hub) handleEnrollInner(w http.ResponseWriter, r *http.Request) {
	url := h.canonicalHubURL(r)
	unix, windows := enrollOneLiners(url, h.token)
	tsUnix, tsWin := tailscaleSetupOneLiners()
	writeJSON(w, http.StatusOK, map[string]any{
		"hubUrl":           url,
		"token":            h.token,
		"unix":             unix,
		"windows":          windows,
		"tailscaleUnix":    tsUnix,
		"tailscaleWindows": tsWin,
	})
}
