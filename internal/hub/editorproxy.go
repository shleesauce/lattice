package hub

import (
	"errors"
	"net"
	"net/http"
	"net/url"
	"strings"

	"time"

	"github.com/shleesauce/lattice/internal/proto"
	"github.com/shleesauce/lattice/internal/tunnel"
)

// editorIdleConnTimeout bounds how long a pooled editor stream lingers idle
// before the Transport closes it (and the agent tears down its code-server dial).
const editorIdleConnTimeout = 90 * time.Second

// isWebSocketUpgrade reports whether the request is a WebSocket upgrade.
func isWebSocketUpgrade(r *http.Request) bool {
	return strings.EqualFold(r.Header.Get("Upgrade"), "websocket")
}

// editorBackendHost is the placeholder authority the hub puts on proxied editor
// requests. The real dial never uses it — our Transport.DialContext ignores the
// address and opens a yamux stream to the agent instead — but net/http needs a
// non-empty URL host and a Host header. code-server is launched with
// --trusted-origins="*" (see agent/editor.go) so it won't 403 on the value.
const editorBackendHost = "127.0.0.1"

// handleEditorProxy reverse-proxies /editor/{sessionId}/* to the code-server that
// the session's agent hosts on loopback, tunneling every request over a yamux
// stream (D27). It is the proven P1 spike recipe — prefix strip + X-Forwarded-*,
// relative-Location rewrite, the trailing-slash 302, and a manual WebSocket
// hijack — with the ONE difference that the transport dials a tunnel stream
// (carrying a sessionId handshake) rather than a TCP socket.
func (h *Hub) handleEditorProxy(w http.ResponseWriter, r *http.Request) {
	rest := strings.TrimPrefix(r.URL.Path, "/editor/")
	sessionID, _, hasSlash := strings.Cut(rest, "/")
	if sessionID == "" {
		http.NotFound(w, r)
		return
	}
	prefix := "/editor/" + sessionID

	rec, ok, err := h.store.GetSession(sessionID)
	if err != nil {
		http.Error(w, "session lookup failed", http.StatusInternalServerError)
		return
	}
	if !ok || proto.SessionKind(rec.Kind) != proto.SessionEditor {
		http.NotFound(w, r)
		return
	}
	if rec.Status == proto.SessionExited {
		http.Error(w, "editor session has ended", http.StatusGone)
		return
	}
	if _, ok := h.registry.getTunnel(rec.AgentID); !ok {
		http.Error(w, "editor tunnel offline", http.StatusBadGateway)
		return
	}

	// Trailing-slash 302 (MANDATORY): code-server emits relative asset URLs, so the
	// browser must sit under /editor/{id}/ for them to resolve under the prefix.
	if !hasSlash {
		target := prefix + "/"
		if r.URL.RawQuery != "" {
			target += "?" + r.URL.RawQuery
		}
		http.Redirect(w, r, target, http.StatusFound)
		return
	}

	dial := func() (net.Conn, error) { return h.dialEditorStream(rec.AgentID, sessionID) }
	fwdProto := forwardedProto(r)

	if isWebSocketUpgrade(r) {
		tunnelWebSocket(w, r, prefix, fwdProto, dial)
		return
	}

	tunnelReverseProxy(prefix, fwdProto, dial, "editor proxy: session="+sessionID).ServeHTTP(w, r)
}

// rewriteEditorLocation resolves code-server's redirect Location headers so the
// browser stays under /editor/{id}/. code-server 302s with a RELATIVE
// "./?folder=…"; left alone the browser would land back at the hub root and loop.
func rewriteEditorLocation(prefix string) func(*http.Response) error {
	return func(resp *http.Response) error {
		loc := resp.Header.Get("Location")
		if loc == "" {
			return nil
		}
		parsed, err := url.Parse(loc)
		if err != nil || parsed.IsAbs() {
			return nil // unparseable or absolute-elsewhere: leave it
		}
		if strings.HasPrefix(loc, "/") {
			if !strings.HasPrefix(loc, prefix) {
				resp.Header.Set("Location", prefix+loc)
			}
			return nil
		}
		base, _ := url.Parse("http://ignored" + prefix + "/")
		resp.Header.Set("Location", base.ResolveReference(parsed).RequestURI())
		return nil
	}
}

// dialEditorStream opens a fresh yamux stream to the agent and writes the
// sessionId handshake so the agent routes it to the right code-server.
func (h *Hub) dialEditorStream(agentID, sessionID string) (net.Conn, error) {
	sess, ok := h.registry.getTunnel(agentID)
	if !ok {
		return nil, errors.New("editor tunnel offline")
	}
	stream, err := sess.OpenStream()
	if err != nil {
		return nil, err
	}
	if err := tunnel.WriteStreamHeader(stream, sessionID); err != nil {
		_ = stream.Close()
		return nil, err
	}
	return stream, nil
}
