package hub

import (
	"bytes"
	"embed"
	"net/http"
	"path"
	"regexp"
	"text/template"
)

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
	// http://mini-ops.tail3c8bee.ts.net:7400 (no trailing slash).
	HubURL string
	// HubHostPort is HubURL stripped of its scheme, e.g.
	// mini-ops.tail3c8bee.ts.net:7400 — the value passed to `agent --hub`.
	HubHostPort string
}

// hubURL reconstructs the externally-reachable hub URL from the request. We
// only serve plain http for self-hosted meshes; if the hub is fronted by TLS
// the operator can override via X-Forwarded-Proto.
func hubURL(r *http.Request) string {
	scheme := "http"
	if proto := r.Header.Get("X-Forwarded-Proto"); proto == "https" {
		scheme = "https"
	} else if r.TLS != nil {
		scheme = "https"
	}
	return scheme + "://" + r.Host
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
	w.Header().Set("Content-Type", "application/octet-stream")
	http.ServeFile(w, r, path.Join(h.distDir, name))
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
	url := hubURL(r)
	data := installData{HubURL: url, HubHostPort: r.Host}
	var buf bytes.Buffer
	if err := installTmpls.ExecuteTemplate(&buf, name, data); err != nil {
		http.Error(w, "installer render failed", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", contentType)
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(buf.Bytes())
}

// handleEnroll returns the copy-paste enrollment one-liners for the dashboard
// onboarding panel. The current hub token is included so the operator can hand
// it straight to a new machine.
func (h *Hub) handleEnroll(w http.ResponseWriter, r *http.Request) {
	url := hubURL(r)
	unix := "curl -fsSL " + url + "/install.sh | sh -s -- --token " + h.token
	windows := "$env:LATTICE_TOKEN='" + h.token + "'; irm " + url + "/install.ps1 | iex"
	writeJSON(w, http.StatusOK, map[string]any{
		"hubUrl":  url,
		"token":   h.token,
		"unix":    unix,
		"windows": windows,
	})
}
