package hub

import (
	"context"
	"errors"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"

	"time"

	"github.com/dylanstoryyy/lattice/internal/proto"
	"github.com/dylanstoryyy/lattice/internal/tunnel"
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

	if isWebSocketUpgrade(r) {
		h.proxyEditorWebSocket(w, r, rec.AgentID, sessionID, prefix)
		return
	}

	fwdProto := "http"
	if r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https" {
		fwdProto = "https"
	}

	rp := &httputil.ReverseProxy{
		Director: func(req *http.Request) {
			origHost := req.Host
			req.URL.Scheme = "http"
			req.URL.Host = editorBackendHost
			req.Host = editorBackendHost
			req.Header.Set("X-Forwarded-Host", origHost)
			req.Header.Set("X-Forwarded-Proto", fwdProto)
			req.Header.Set("X-Forwarded-Prefix", prefix+"/")
			p := strings.TrimPrefix(req.URL.Path, prefix)
			if p == "" {
				p = "/"
			}
			req.URL.Path = p
		},
		ModifyResponse: rewriteEditorLocation(prefix),
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
				return h.dialEditorStream(rec.AgentID, sessionID)
			},
			// One code-server, one user: a small idle pool keeps the workbench's
			// asset burst snappy without holding many streams open forever.
			MaxIdleConns:        8,
			IdleConnTimeout:     editorIdleConnTimeout,
			DisableCompression:  true,
			TLSHandshakeTimeout: 0,
		},
		ErrorHandler: func(w http.ResponseWriter, _ *http.Request, e error) {
			log.Printf("editor proxy: session=%s: %v", sessionID, e)
			http.Error(w, "editor backend error: "+e.Error(), http.StatusBadGateway)
		},
	}
	rp.ServeHTTP(w, r)
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

// proxyEditorWebSocket tunnels a WebSocket upgrade (the code-server extension host
// + terminals run over WS). httputil.ReverseProxy strips hop-by-hop Upgrade/
// Connection headers, so the upgrade is done by hand: open a tunnel stream, write
// the rewritten upgrade request, hijack the client, and io.Copy both ways.
func (h *Hub) proxyEditorWebSocket(w http.ResponseWriter, r *http.Request, agentID, sessionID, prefix string) {
	backendPath := strings.TrimPrefix(r.URL.Path, prefix)
	if backendPath == "" {
		backendPath = "/"
	}

	stream, err := h.dialEditorStream(agentID, sessionID)
	if err != nil {
		http.Error(w, "editor tunnel dial failed: "+err.Error(), http.StatusBadGateway)
		return
	}
	defer stream.Close()

	reqURI := backendPath
	if r.URL.RawQuery != "" {
		reqURI += "?" + r.URL.RawQuery
	}
	upstreamReq, err := http.NewRequest(r.Method, "http://"+editorBackendHost+reqURI, nil)
	if err != nil {
		http.Error(w, "request build failed", http.StatusInternalServerError)
		return
	}
	for k, vv := range r.Header {
		for _, v := range vv {
			upstreamReq.Header.Add(k, v)
		}
	}
	fwdProto := "http"
	if r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https" {
		fwdProto = "https"
	}
	upstreamReq.Header.Set("X-Forwarded-Host", r.Host)
	upstreamReq.Header.Set("X-Forwarded-Proto", fwdProto)
	upstreamReq.Header.Set("X-Forwarded-Prefix", prefix+"/")
	upstreamReq.Host = editorBackendHost

	if err := upstreamReq.Write(stream); err != nil {
		http.Error(w, "upstream write failed: "+err.Error(), http.StatusBadGateway)
		return
	}

	hj, ok := w.(http.Hijacker)
	if !ok {
		http.Error(w, "hijack not supported", http.StatusInternalServerError)
		return
	}
	clientConn, buf, err := hj.Hijack()
	if err != nil {
		http.Error(w, "hijack failed: "+err.Error(), http.StatusInternalServerError)
		return
	}
	defer clientConn.Close()

	// Flush any bytes the client already sent past the request line into the tunnel.
	if buf.Reader.Buffered() > 0 {
		pending := make([]byte, buf.Reader.Buffered())
		if _, err := io.ReadFull(buf.Reader, pending); err == nil {
			stream.Write(pending)
		}
	}

	done := make(chan struct{}, 2)
	go func() { io.Copy(stream, clientConn); done <- struct{}{} }()
	go func() { io.Copy(clientConn, stream); done <- struct{}{} }()
	<-done
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
