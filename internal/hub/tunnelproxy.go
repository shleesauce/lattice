package hub

import (
	"context"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httputil"
	"strings"
)

// This file holds the machinery shared by the editor (D27) and preview (D32)
// reverse proxies. Both tunnel HTTP — and a hand-rolled WebSocket upgrade — over
// a yamux stream to an agent's loopback service; they differ ONLY in how the
// stream is dialed (the editor carries a sessionId handshake, the preview a
// port) and in the path prefix they strip. Keeping the proxy body here means the
// Director, the Transport pool, the Location rewrite, AND the security-sensitive
// header handling on the WebSocket path are defined once for both.

// tunnelDial opens a fresh yamux stream to an agent's loopback service. The
// concrete dialer (editor vs preview) is captured by the caller; this proxy code
// never knows which kind of backend it is reaching.
type tunnelDial func() (net.Conn, error)

// tunnelReverseProxy builds the non-WebSocket reverse proxy shared by editor and
// preview. prefix is the path segment to strip (e.g. "/editor/{id}" or
// "/preview/{agent}/{port}"); fwdProto is the X-Forwarded-Proto value to set;
// dial opens the per-request tunnel stream; errLabel prefixes the ErrorHandler
// log line. httputil.ReverseProxy already strips hop-by-hop headers on this path,
// so the smuggling surface lives only on the WebSocket path below.
func tunnelReverseProxy(prefix, fwdProto string, dial tunnelDial, errLabel string) *httputil.ReverseProxy {
	return &httputil.ReverseProxy{
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
				return dial()
			},
			// One backend, one user: a small idle pool keeps the asset burst snappy
			// without holding many streams open forever.
			MaxIdleConns:        8,
			IdleConnTimeout:     editorIdleConnTimeout,
			DisableCompression:  true,
			TLSHandshakeTimeout: 0,
		},
		ErrorHandler: func(w http.ResponseWriter, _ *http.Request, e error) {
			log.Printf("%s: %v", errLabel, e)
			http.Error(w, "backend error: "+e.Error(), http.StatusBadGateway)
		},
	}
}

// hopByHopHeaders are the standard connection-scoped headers that MUST NOT be
// forwarded to an upstream (RFC 7230 §6.1). httputil.ReverseProxy strips these on
// the non-WS path; the hand-rolled WebSocket path must do the same or it becomes a
// request-smuggling / header-injection surface.
var hopByHopHeaders = []string{
	"Connection",
	"Keep-Alive",
	"Proxy-Authenticate",
	"Proxy-Authorization",
	"Te",
	"Trailer",
	"Transfer-Encoding",
	"Upgrade",
}

// tunnelWebSocket performs the WebSocket upgrade hand-roll shared by editor and
// preview. httputil.ReverseProxy strips the hop-by-hop Upgrade/Connection headers
// the handshake needs, so the upgrade is done by hand: open a tunnel stream, write
// a SANITIZED upgrade request, hijack the client, and io.Copy both ways.
//
// SECURITY: we do NOT blind-copy the client's headers onto the upstream request
// (the old code did, which let a client inject Transfer-Encoding/Content-Length or
// smuggle a second request onto the raw tunnel). Instead we copy the client
// headers, strip every hop-by-hop header — including any header NAMED in the
// client's Connection value — drop Content-Length, then re-add only the WebSocket
// handshake headers the upstream actually requires.
func tunnelWebSocket(w http.ResponseWriter, r *http.Request, prefix, fwdProto string, dial tunnelDial) {
	backendPath := strings.TrimPrefix(r.URL.Path, prefix)
	if backendPath == "" {
		backendPath = "/"
	}

	stream, err := dial()
	if err != nil {
		http.Error(w, "tunnel dial failed: "+err.Error(), http.StatusBadGateway)
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

	// Copy client headers, then strip everything connection-scoped.
	for k, vv := range r.Header {
		for _, v := range vv {
			upstreamReq.Header.Add(k, v)
		}
	}
	// Drop any header the client listed in its Connection header (these are, by
	// definition, hop-by-hop and must not cross the proxy).
	for _, tok := range strings.Split(r.Header.Get("Connection"), ",") {
		if name := strings.TrimSpace(tok); name != "" {
			upstreamReq.Header.Del(name)
		}
	}
	for _, h := range hopByHopHeaders {
		upstreamReq.Header.Del(h)
	}
	// Never let the client dictate framing onto the raw tunnel.
	upstreamReq.Header.Del("Content-Length")
	upstreamReq.ContentLength = 0

	// Re-add only the WebSocket handshake headers the upstream needs, taken from
	// the validated client request.
	upstreamReq.Header.Set("Upgrade", "websocket")
	upstreamReq.Header.Set("Connection", "Upgrade")
	copyHeaderIfPresent(upstreamReq, r, "Sec-Websocket-Key")
	copyHeaderIfPresent(upstreamReq, r, "Sec-Websocket-Version")
	copyHeaderIfPresent(upstreamReq, r, "Sec-Websocket-Protocol")
	copyHeaderIfPresent(upstreamReq, r, "Sec-Websocket-Extensions")

	// Forwarding context the backend expects (mirrors the Director on the HTTP path).
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
			_, _ = stream.Write(pending) // best-effort; a failed write surfaces as a copy error below
		}
	}

	// Relay both directions. Either copy ending (EOF or error) means one side
	// closed; the deferred Close on the client conn plus the caller's stream close
	// then unblock the other copy. Copy errors are the normal termination signal
	// for a hijacked tunnel, so they are intentionally not surfaced.
	done := make(chan struct{}, 2)
	go func() { _, _ = io.Copy(stream, clientConn); done <- struct{}{} }()
	go func() { _, _ = io.Copy(clientConn, stream); done <- struct{}{} }()
	<-done
}

// copyHeaderIfPresent copies a single header from src to dst only when src set it,
// so we never invent a handshake header the client did not send.
func copyHeaderIfPresent(dst, src *http.Request, name string) {
	if v := src.Header.Get(name); v != "" {
		dst.Header.Set(name, v)
	}
}

// forwardedProto returns "https" when the request arrived (or was forwarded) over
// TLS, else "http" — the X-Forwarded-Proto value both proxies set.
func forwardedProto(r *http.Request) string {
	if r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https" {
		return "https"
	}
	return "http"
}
