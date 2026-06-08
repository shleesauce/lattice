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
// Asset-path caveat: dev servers that emit ROOT-ABSOLUTE asset URLs (Vite's
// /@vite/client, Next's /_next/…) ignore the /preview/{agent}/{port}/ prefix and
// 404 against the hub. The fix is on the dev server (set Vite `base` / Next
// `basePath`+`assetPrefix` to the prefix, or a relative base); the proxy stays a
// transparent tunnel and does not rewrite app payloads.
func (h *Hub) handlePreviewProxy(w http.ResponseWriter, r *http.Request) {
	rest := strings.TrimPrefix(r.URL.Path, "/preview/")
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
	prefix := "/preview/" + agentID + "/" + portStr

	if _, ok := h.registry.getTunnel(agentID); !ok {
		http.Error(w, "agent tunnel offline", http.StatusBadGateway)
		return
	}

	// Trailing-slash 302 (MANDATORY): dev servers that emit relative asset URLs
	// need the browser to sit under /preview/{agent}/{port}/ to resolve them.
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

	// dev-server HMR is a WebSocket — mandatory or hot-reload hangs.
	if isWebSocketUpgrade(r) {
		tunnelWebSocket(w, r, prefix, fwdProto, dial)
		return
	}

	tunnelReverseProxy(prefix, fwdProto, dial, "preview proxy: agent="+agentID+" port="+portStr).ServeHTTP(w, r)
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
