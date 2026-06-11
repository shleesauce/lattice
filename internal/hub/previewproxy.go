package hub

import (
	"errors"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"strings"

	"github.com/shleesauce/lattice/internal/tunnel"
)

// Preview-port policy (FIX 4 — SSRF containment). The preview proxy lets an
// authenticated operator reach 127.0.0.1:<port> on ANY agent over the tunnel. If
// the only check were "1 ≤ port ≤ 65535" the feature would be a loopback SSRF
// pivot: a request could be aimed at an agent's SSH (22), a database (5432/3306),
// a metadata/admin service, or any other internal listener bound to localhost.
// Preview is for DEV SERVERS, which conventionally live in a well-known high
// range, so we restrict the proxy to that range by default. The range is
// configurable per installation (config.json previewPortMin/Max) so a self-hoster
// whose dev servers live elsewhere can widen/narrow it without editing code.
const (
	defaultPreviewPortMin = 3000
	defaultPreviewPortMax = 9999
)

// isAllowedPreviewPort reports whether port is inside this hub's configured
// dev-server range. Keeping it a method (not an inline comparison) leaves room
// for a future per-agent declared-port allowlist.
func (h *Hub) isAllowedPreviewPort(port int) bool {
	return port >= h.previewPortMin && port <= h.previewPortMax
}

// handlePreviewProxy reverse-proxies /preview/{agentId}/{port}/* to a dev server
// the agent hosts on its own loopback, tunneling every request over a yamux
// stream (D27 / D32). It is the SAME proven recipe as the editor proxy — see
// tunnelproxy.go for the shared Director / Transport / WebSocket machinery — but
// keyed by (agent, port) rather than an editor session, since a dev server
// belongs to a MACHINE, not a code session. The agent dials 127.0.0.1:port
// directly; nothing new is exposed on its external interface (D2).
//
// STRIP mode: the proxy strips /preview/{agent}/{port} before forwarding, so the
// backend sees /. Works for any dev server that emits RELATIVE asset URLs (plain
// http.server, code-server-style apps). Framework dev servers that emit
// ROOT-ABSOLUTE asset URLs (Vite's /@vite/client, Next's /_next/…) ignore the
// prefix and 404 — use /fpreview/ (no-strip) for those instead.
func (h *Hub) handlePreviewProxy(w http.ResponseWriter, r *http.Request) {
	h.servePreview(w, r, "/preview/", true)
}

// handleFrameworkPreviewProxy reverse-proxies /fpreview/{agentId}/{port}/* in
// NO-STRIP mode for framework dev servers (Vite / Next). The dev server is launched
// with its base path set to the SAME /fpreview/{agent}/{port}/ prefix (Vite
// `--base`, Next `basePath`+`assetPrefix`), so it already emits correctly-prefixed
// asset + HMR URLs; the proxy forwards the FULL path unchanged. Stripping would make
// a base-configured Vite 302 `/` → its base, which the strip undoes → infinite loop
// (see docs/NEXT-tunneled-preview.md). The exact base string is surfaced in the dock
// for copy-paste; it's per-machine (the agentId is hostname-os) so it can't live in a
// committed config.
func (h *Hub) handleFrameworkPreviewProxy(w http.ResponseWriter, r *http.Request) {
	h.servePreview(w, r, "/fpreview/", false)
}

// servePreview is the shared body for both preview modes. route is the leading
// path segment ("/preview/" or "/fpreview/"); strip controls whether that prefix is
// removed before the request is forwarded to the agent's dev server. The browser
// always stays under route (the trailing-slash 302 + every asset request), so the
// mode is encoded in the PATH — a request and all the assets it pulls share it.
func (h *Hub) servePreview(w http.ResponseWriter, r *http.Request, route string, strip bool) {
	rest := strings.TrimPrefix(r.URL.Path, route)
	agentID, after, _ := strings.Cut(rest, "/")
	portStr, _, hasSlash := strings.Cut(after, "/")
	if agentID == "" || portStr == "" {
		http.NotFound(w, r)
		return
	}
	port, err := strconv.Atoi(portStr)
	if err != nil || port < 1 || port > 65535 {
		http.Error(w, "invalid preview port", http.StatusBadRequest)
		return
	}
	// SSRF containment: only dev-server ports may be proxied (see policy above).
	if !h.isAllowedPreviewPort(port) {
		http.Error(w, fmt.Sprintf("preview port not allowed (dev-server range is %d-%d)", h.previewPortMin, h.previewPortMax), http.StatusForbidden)
		return
	}
	prefix := strings.TrimSuffix(route, "/") + "/" + agentID + "/" + portStr

	if _, ok := h.registry.getTunnel(agentID); !ok {
		http.Error(w, "agent tunnel offline", http.StatusBadGateway)
		return
	}

	// Trailing-slash 302 (MANDATORY): the browser must sit under the prefix so the
	// dev server's asset URLs (relative in strip mode, base-prefixed in no-strip)
	// resolve back through the same route.
	if !hasSlash {
		target := prefix + "/"
		if r.URL.RawQuery != "" {
			target += "?" + r.URL.RawQuery
		}
		http.Redirect(w, r, target, http.StatusFound)
		return
	}

	dial := func() (net.Conn, error) { return h.dialPreviewStream(agentID, port) }
	fwdProto := forwardedProto(r)

	// In no-strip mode the FULL path is forwarded (the base-configured backend
	// expects its prefix); an empty strip-prefix leaves req.URL.Path untouched.
	stripPrefix := prefix
	if !strip {
		stripPrefix = ""
	}

	// dev-server HMR is a WebSocket — mandatory or hot-reload hangs.
	if isWebSocketUpgrade(r) {
		tunnelWebSocket(w, r, stripPrefix, fwdProto, dial)
		return
	}

	tunnelReverseProxy(stripPrefix, fwdProto, dial, "preview proxy: agent="+agentID+" port="+portStr).ServeHTTP(w, r)
}

// dialPreviewStream opens a fresh yamux stream to the agent and writes the
// preview-port handshake so the agent dials the right loopback dev server.
func (h *Hub) dialPreviewStream(agentID string, port int) (net.Conn, error) {
	sess, ok := h.registry.getTunnel(agentID)
	if !ok {
		return nil, errors.New("agent tunnel offline")
	}
	stream, err := sess.OpenStream()
	if err != nil {
		return nil, err
	}
	if err := tunnel.WritePreviewHeader(stream, port); err != nil {
		_ = stream.Close()
		return nil, err
	}
	return stream, nil
}
