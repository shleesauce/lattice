package hub

// Phase 3 (M3) security hardening: admin auth on top of the bcrypt password hash
// collected by the first-run wizard (Phase 2). Auth is enforced ONLY when an admin
// password hash exists (h.adminPasswordHash != ""). On a legacy hub with no hash,
// every middleware here is a pass-through and the hub behaves exactly as before
// (open) — the non-breaking migration path. A legacy operator opts in with
// `lattice hub set-password`.
//
// Two credentials authenticate an admin request:
//   - a valid session cookie (lattice_session), minted by POST /api/auth/login or
//     by finishing the setup wizard, OR
//   - Authorization: Bearer <enrollToken>. The Bearer token IS the enrollment
//     token (h.token): single-operator, the long-lived API token == the enroll
//     token, so curl/scripts authenticate with the same secret agents enroll with.

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/bcrypt"
)

// sessionTTL is how long a login session (and its cookie) stays valid.
const sessionTTL = 30 * 24 * time.Hour

// sessionCookie is the name of the session cookie set on login.
const sessionCookie = "lattice_session"

// sessionCleanupInterval is how often expired sessions are swept from the store.
const sessionCleanupInterval = 1 * time.Hour

// Login rate-limit knobs: at most maxLoginFails failed attempts per loginWindow
// per client IP before further attempts are rejected with 429.
const maxLoginFails = 10
const loginWindow = 5 * time.Minute

// sessionStore holds live login sessions: token → expiry. Tokens are 32 random
// bytes hex. It is safe for concurrent use.
type sessionStore struct {
	mu       sync.Mutex
	sessions map[string]time.Time
}

func newSessionStore() *sessionStore {
	return &sessionStore{sessions: make(map[string]time.Time)}
}

// create mints a new session token (32 random bytes hex), stores it with an
// expiry of now+sessionTTL, and returns it. It PANICS if crypto/rand is
// unavailable rather than minting a predictable, time-seeded session token (the
// old fallback): a guessable session token is an auth bypass, and rand.Read never
// fails on the platforms Lattice targets, so refusing is the safe failure. Keeping
// the string signature avoids rippling into the setup-wizard auto-login caller.
func (s *sessionStore) create() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		panic("hub: crypto/rand unavailable minting session token: " + err.Error())
	}
	tok := hex.EncodeToString(b)
	s.mu.Lock()
	s.sessions[tok] = time.Now().Add(sessionTTL)
	s.mu.Unlock()
	return tok
}

// valid reports whether token exists and has not expired.
func (s *sessionStore) valid(token string) bool {
	if token == "" {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	exp, ok := s.sessions[token]
	if !ok {
		return false
	}
	if time.Now().After(exp) {
		delete(s.sessions, token)
		return false
	}
	return true
}

// revoke drops a session token (logout).
func (s *sessionStore) revoke(token string) {
	if token == "" {
		return
	}
	s.mu.Lock()
	delete(s.sessions, token)
	s.mu.Unlock()
}

// cleanup drops every expired session.
func (s *sessionStore) cleanup() {
	now := time.Now()
	s.mu.Lock()
	for tok, exp := range s.sessions {
		if now.After(exp) {
			delete(s.sessions, tok)
		}
	}
	s.mu.Unlock()
}

// sessionCleanupLoop sweeps expired sessions hourly until ctx is cancelled,
// matching the other background loops (sweepLoop, reapLoop, …). It also sweeps
// the login rate-limiter's per-IP failure map so stale IP buckets (an attacker
// who fails a few times and never returns) don't accumulate without bound.
func (h *Hub) sessionCleanupLoop(ctx context.Context) {
	ticker := time.NewTicker(sessionCleanupInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			h.sessions.cleanup()
			h.loginLimiter.sweep()
			h.capLimiter.sweep()
		}
	}
}

// setSessionCookie writes the session cookie. It is intentionally NOT Secure:
// the hub is served over plain HTTP across a Tailscale tailnet (no TLS at the
// hub), so a Secure cookie would be dropped by the browser and the operator
// could never stay logged in. HttpOnly + SameSite=Strict still keep it out of
// JS and off cross-site requests.
func setSessionCookie(w http.ResponseWriter, token string) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookie,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
		MaxAge:   int(sessionTTL / time.Second),
	})
}

// clearSessionCookie expires the session cookie (logout).
func clearSessionCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookie,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
		MaxAge:   -1,
	})
}

// authEnabled reports whether admin auth is enforced. It is enforced exactly
// when an admin password hash is configured; an empty hash (legacy hub) means
// every gate is a pass-through.
func (h *Hub) authEnabled() bool {
	h.cfgMu.RLock()
	defer h.cfgMu.RUnlock()
	return h.adminPasswordHash != ""
}

// authed reports whether the request carries a valid admin credential: a Bearer
// token equal (constant-time) to the enrollment token, or a live session cookie.
func (h *Hub) authed(r *http.Request) bool {
	if h.bearerIsMasterToken(r) {
		return true
	}
	if c, err := r.Cookie(sessionCookie); err == nil {
		if h.sessions.valid(c.Value) {
			return true
		}
	}
	return false
}

// bearerIsMasterToken reports whether the request carries an Authorization:
// Bearer header whose value is the master token (constant-time). It is the single
// reader of the Bearer convention so the master-token-as-API-credential rule (D37)
// lives in one place — used both by authed (admin auth) and by requireAuthOrToken
// (the auth-disabled fallback for credential-bearing endpoints).
func (h *Hub) bearerIsMasterToken(r *http.Request) bool {
	authz := r.Header.Get("Authorization")
	if !strings.HasPrefix(authz, "Bearer ") {
		return false
	}
	presented := strings.TrimSpace(strings.TrimPrefix(authz, "Bearer "))
	return h.matchesMasterToken(presented)
}

// requireAuth wraps a handler so it is reachable only when auth is disabled
// (legacy hub) or the request is authenticated. Unauthenticated requests on a
// password-protected hub get a generic 401 with a Bearer challenge.
func (h *Hub) requireAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !h.authEnabled() || h.authed(r) {
			next(w, r)
			return
		}
		w.Header().Set("WWW-Authenticate", "Bearer")
		writeJSON(w, http.StatusUnauthorized, map[string]any{"error": "unauthorized"})
	}
}

// requirePrivileged enforces a credential for an RCE-class action (remote command
// exec, fleet-wide auto-update, machine power-off) EVEN on a passwordless hub, and
// reports whether the request may proceed.
//
// The hole it closes: requireAuth is a pass-through when no admin password is set
// (the legacy "open on a trusted network" mode), so on a passwordless hub anyone who
// can reach the port could POST /api/agents/{id}/exec and run arbitrary commands on
// every fleet machine — open, unauthenticated, fleet-wide RCE over the tailnet.
// These specific actions are too dangerous to ride the auth-off pass-through, so we
// fail closed: when auth is OFF we still demand the master token as a Bearer
// credential (the operator holds it — it's the same secret agents enroll with — but
// an arbitrary tailnet peer does not). When auth is ON, requireAuth already gated the
// route, so this is a no-op pass. The trade (mirroring requireAuthOrToken): a
// passwordless browser dashboard can no longer fire these without the token; the
// operator should set a password (cookie auth) or present the token. On failure it
// writes a 401 and returns false so the handler can `if !h.requirePrivileged(...) { return }`.
func (h *Hub) requirePrivileged(w http.ResponseWriter, r *http.Request) bool {
	if h.authEnabled() {
		// requireAuth already verified the session/Bearer for this route.
		return true
	}
	if h.bearerIsMasterToken(r) {
		return true
	}
	w.Header().Set("WWW-Authenticate", "Bearer")
	writeJSON(w, http.StatusUnauthorized, map[string]any{"error": "this action requires the hub token"})
	return false
}

// requestIsLoopback reports whether r originates from the loopback interface (the
// operator is on the hub box itself). Used to let the first-run setup wizard run
// unauthenticated from localhost while still blocking a remote tailnet peer from
// claiming admin before a password exists. RemoteAddr is the real peer (the hub is
// reached directly over the tailnet, no trusted proxy), so this can't be spoofed by
// a client header.
func requestIsLoopback(r *http.Request) bool {
	ip := net.ParseIP(clientIP(r))
	return ip != nil && ip.IsLoopback()
}

// requireAuthOrToken gates a credential-bearing endpoint (e.g. handleEnroll, which
// hands out the master token). When admin auth is ENABLED it behaves exactly like
// requireAuth (session cookie OR master-token Bearer). When admin auth is DISABLED
// — a legacy hub with no admin password — it does NOT fall through to open: it
// still requires the master token as a Bearer credential. This closes the
// auth-off-by-default hole where anyone who could reach the port could read the
// master token back out of /api/enroll. The trade: an unauthenticated dashboard on
// a passwordless hub can no longer fetch the enroll one-liners; the operator must
// either set a password (proper sessions) or present the token they already hold.
func (h *Hub) requireAuthOrToken(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if h.authEnabled() {
			if h.authed(r) {
				next(w, r)
				return
			}
		} else if h.bearerIsMasterToken(r) {
			next(w, r)
			return
		}
		w.Header().Set("WWW-Authenticate", "Bearer")
		writeJSON(w, http.StatusUnauthorized, map[string]any{"error": "unauthorized"})
	}
}

// capLimitMax / capLimitWindow bound the ungated capability endpoints
// (/api/hooks/state, /api/approvals/{nonce}) per client IP. Generous on purpose:
// hooks fire on every claude turn, so a heavy multi-session box must never trip it,
// while a flood from one IP is still capped. 300/min ≈ 5/s sustained per IP.
const capLimitMax = 300
const capLimitWindow = 1 * time.Minute

// rateLimiter is a generic per-IP fixed-window request limiter: at most max
// requests per window per IP. Unlike loginLimiter (which counts only failures), this
// counts every call — used to throttle the ungated capability endpoints. Safe for
// concurrent use.
type rateLimiter struct {
	mu     sync.Mutex
	max    int
	window time.Duration
	hits   map[string][]time.Time
}

func newRateLimiter(max int, window time.Duration) *rateLimiter {
	return &rateLimiter{max: max, window: window, hits: make(map[string][]time.Time)}
}

// allow records a request from ip and reports whether it is within the limit. It
// prunes the IP's window in place as it goes, so a quiet IP's bucket is dropped. A
// nil limiter allows everything (so a Hub built without one in a test never blocks).
func (l *rateLimiter) allow(ip string) bool {
	if l == nil {
		return true
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	cutoff := time.Now().Add(-l.window)
	kept := l.hits[ip][:0]
	for _, t := range l.hits[ip] {
		if t.After(cutoff) {
			kept = append(kept, t)
		}
	}
	if len(kept) >= l.max {
		l.hits[ip] = kept // over the cap: keep the window, reject
		return false
	}
	l.hits[ip] = append(kept, time.Now())
	return true
}

// sweep drops per-IP buckets with no request inside the trailing window, bounding
// the map by distinct recent IPs (mirrors loginLimiter.sweep).
func (l *rateLimiter) sweep() {
	if l == nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	cutoff := time.Now().Add(-l.window)
	for ip, times := range l.hits {
		live := false
		for _, t := range times {
			if t.After(cutoff) {
				live = true
				break
			}
		}
		if !live {
			delete(l.hits, ip)
		}
	}
}

// loginLimiter throttles failed logins per client IP to blunt password guessing.
type loginLimiter struct {
	mu    sync.Mutex
	fails map[string][]time.Time
}

func newLoginLimiter() *loginLimiter {
	return &loginLimiter{fails: make(map[string][]time.Time)}
}

// allow reports whether ip may make another login attempt: it has fewer than
// maxLoginFails recorded failures within the trailing loginWindow.
func (l *loginLimiter) allow(ip string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	cutoff := time.Now().Add(-loginWindow)
	kept := l.fails[ip][:0]
	for _, t := range l.fails[ip] {
		if t.After(cutoff) {
			kept = append(kept, t)
		}
	}
	if len(kept) == 0 {
		delete(l.fails, ip)
	} else {
		l.fails[ip] = kept
	}
	return len(kept) < maxLoginFails
}

// fail records a failed attempt for ip.
func (l *loginLimiter) fail(ip string) {
	l.mu.Lock()
	l.fails[ip] = append(l.fails[ip], time.Now())
	l.mu.Unlock()
}

// reset clears the failure history for ip (on a successful login).
func (l *loginLimiter) reset(ip string) {
	l.mu.Lock()
	delete(l.fails, ip)
	l.mu.Unlock()
}

// sweep drops stale per-IP failure buckets whose every recorded attempt is older
// than the trailing loginWindow. allow() already prunes a bucket when that IP
// retries, but an IP that fails a few times and never comes back would otherwise
// keep its bucket forever — so the map grows per distinct attacker IP without
// bound. The hourly cleanup loop calls this to cap it. A bucket with any live
// (within-window) entry is left intact so it still counts toward the limit.
func (l *loginLimiter) sweep() {
	l.mu.Lock()
	defer l.mu.Unlock()
	cutoff := time.Now().Add(-loginWindow)
	for ip, times := range l.fails {
		live := false
		for _, t := range times {
			if t.After(cutoff) {
				live = true
				break
			}
		}
		if !live {
			delete(l.fails, ip)
		}
	}
}

// clientIP returns the host portion of r.RemoteAddr. XFF is intentionally
// ignored: the hub is reached directly over the tailnet, so RemoteAddr is the
// real peer and trusting a client-supplied header would let an attacker spoof
// the rate-limit bucket.
func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// handleAuthStatus answers GET /api/auth/status (open): whether auth is required
// on this hub and whether the caller is currently authenticated.
func (h *Hub) handleAuthStatus(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"authRequired":  h.authEnabled(),
		"authenticated": h.authed(r),
	})
}

// handleAuthLogin answers POST /api/auth/login (open): validates the admin
// password against the bcrypt hash, and on success mints a session + cookie.
// Rate-limited per IP. All failure responses are generic (no detail) to avoid
// leaking whether auth is configured or whether the password was close.
func (h *Hub) handleAuthLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !h.authEnabled() {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "auth not configured"})
		return
	}
	ip := clientIP(r)
	if !h.loginLimiter.allow(ip) {
		writeJSON(w, http.StatusTooManyRequests, map[string]any{"error": "too many attempts, try again later"})
		return
	}

	var body struct {
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid json body"})
		return
	}

	h.cfgMu.RLock()
	hash := h.adminPasswordHash
	h.cfgMu.RUnlock()

	if bcrypt.CompareHashAndPassword([]byte(hash), []byte(body.Password)) != nil {
		h.loginLimiter.fail(ip)
		writeJSON(w, http.StatusUnauthorized, map[string]any{"error": "invalid password"})
		return
	}

	h.loginLimiter.reset(ip)
	tok := h.sessions.create()
	setSessionCookie(w, tok)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// handleAuthLogout answers POST /api/auth/logout (open): revokes the presented
// session (if any) and clears the cookie.
func (h *Hub) handleAuthLogout(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if c, err := r.Cookie(sessionCookie); err == nil {
		h.sessions.revoke(c.Value)
	}
	clearSessionCookie(w)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// tokenHint returns a non-secret hint for logs: the first 3 chars of the token
// plus an ellipsis (enough to eyeball-match ~/.lattice/.lattice-token), or
// "(none)" when empty. The full token is never logged.
func tokenHint(tok string) string {
	if tok == "" {
		return "(none)"
	}
	if len(tok) <= 3 {
		return tok + "…"
	}
	return tok[:3] + "…"
}
