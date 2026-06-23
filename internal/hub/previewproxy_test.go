package hub

import (
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestPreviewStripVsNoStrip proves the path the dev-server backend actually
// receives in each mode: STRIP (/preview/) removes the /…/{agent}/{port} prefix so
// a plain server sees "/", while NO-STRIP (/fpreview/) forwards the FULL path so a
// base-configured Vite/Next sees its own prefix. It drives the REAL proxy machinery
// (tunnelReverseProxy) with a dial that connects to a local backend, so the Director
// path logic is exercised end-to-end, not reimplemented.
func TestPreviewStripVsNoStrip(t *testing.T) {
	// Backend echoes back the exact path it was asked for.
	var gotPath string
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		_, _ = io.WriteString(w, "ok")
	}))
	defer backend.Close()
	backendAddr := strings.TrimPrefix(backend.URL, "http://")
	dial := func() (net.Conn, error) { return net.Dial("tcp", backendAddr) }

	cases := []struct {
		name        string
		prefix      string // strip-prefix passed to the proxy ("" == no-strip)
		reqPath     string
		wantBackend string
	}{
		{"strip serves index", "/preview/agent-x/5173", "/preview/agent-x/5173/", "/"},
		{"strip serves asset", "/preview/agent-x/5173", "/preview/agent-x/5173/assets/app.js", "/assets/app.js"},
		{"nostrip serves index", "", "/fpreview/agent-x/5173/", "/fpreview/agent-x/5173/"},
		{"nostrip serves vite client", "", "/fpreview/agent-x/5173/@vite/client", "/fpreview/agent-x/5173/@vite/client"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gotPath = ""
			rr := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, tc.reqPath, nil)
			tunnelReverseProxy(tc.prefix, "http", dial, "test").ServeHTTP(rr, req)
			if rr.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200 (body %q)", rr.Code, rr.Body.String())
			}
			if gotPath != tc.wantBackend {
				t.Fatalf("backend saw path %q, want %q", gotPath, tc.wantBackend)
			}
		})
	}
}
