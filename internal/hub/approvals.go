package hub

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"html"
	"log"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/shleesauce/lattice/internal/proto"
)

// approvalTTL bounds how long a phone-approval capability link stays valid after a
// session goes idle. Long enough to pick up the phone and act; short enough that a
// leaked link (the nonce is the only credential it carries) is dead soon after.
const approvalTTL = 60 * time.Minute

// pendingApproval is one armed approve/deny capability, keyed by an unguessable
// nonce carried in the ntfy action URL. Single-shot: consumed on first use.
type pendingApproval struct {
	sessionID string
	agentID   string
	createdAt time.Time
}

// approvalStore holds armed approvals in memory. Deliberately NOT persisted: an
// approval is only meaningful while the session sits waiting, and a hub restart
// that drops them just yields an "expired" link — safe (reopen the session to
// re-arm). It also tracks hub-initiated exits so a user-closed session doesn't
// emit a "finished" push. Both maps are swept on the hub's existing reap tick.
type approvalStore struct {
	mu       sync.Mutex
	pending  map[string]pendingApproval
	expected map[string]time.Time // sessionId → when the hub asked it to close
}

func newApprovalStore() *approvalStore {
	return &approvalStore{
		pending:  make(map[string]pendingApproval),
		expected: make(map[string]time.Time),
	}
}

// mint arms a fresh single-use approval for a waiting session and returns its nonce.
func (a *approvalStore) mint(sessionID, agentID string, now time.Time) string {
	nonce := randomNonce()
	a.mu.Lock()
	a.pending[nonce] = pendingApproval{sessionID: sessionID, agentID: agentID, createdAt: now}
	a.mu.Unlock()
	return nonce
}

// consume returns and removes the approval for a nonce, rejecting unknown/expired.
func (a *approvalStore) consume(nonce string, now time.Time) (pendingApproval, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	p, ok := a.pending[nonce]
	if !ok {
		return pendingApproval{}, false
	}
	delete(a.pending, nonce)
	if now.Sub(p.createdAt) > approvalTTL {
		return pendingApproval{}, false
	}
	return p, true
}

// hasForSession reports whether the session currently has an armed approval — i.e.
// it is blocked waiting on the operator's decision. Drives the dashboard's red
// "needs you" status dot (BUG-009). Reuses the approval lifecycle (armed on the
// awaiting edge, dropped on resume/consume/sweep), so there's no separate state to
// go stale.
func (a *approvalStore) hasForSession(sessionID string) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	for _, p := range a.pending {
		if p.sessionID == sessionID {
			return true
		}
	}
	return false
}

// dropForSession disarms any approvals for a session that resumed or ended, so a
// stale tap can't inject into a session that's no longer waiting on input.
func (a *approvalStore) dropForSession(sessionID string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	for n, p := range a.pending {
		if p.sessionID == sessionID {
			delete(a.pending, n)
		}
	}
}

// expectExit marks a session the hub itself is closing, so onSessionExit can tell
// a user-initiated close (no "finished" ping) from an unexpected death (worth one).
func (a *approvalStore) expectExit(sessionID string, now time.Time) {
	a.mu.Lock()
	a.expected[sessionID] = now
	a.mu.Unlock()
}

// takeExpected reports (and clears) whether this exit was hub-initiated.
func (a *approvalStore) takeExpected(sessionID string) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	_, ok := a.expected[sessionID]
	delete(a.expected, sessionID)
	return ok
}

// sweep drops expired nonces and stale expected-exit markers.
func (a *approvalStore) sweep(now time.Time) {
	a.mu.Lock()
	defer a.mu.Unlock()
	for n, p := range a.pending {
		if now.Sub(p.createdAt) > approvalTTL {
			delete(a.pending, n)
		}
	}
	for id, t := range a.expected {
		if now.Sub(t) > approvalTTL {
			delete(a.expected, id)
		}
	}
}

// randomNonce mints an unguessable, URL-safe capability token. A crypto/rand
// failure is fatal — better to refuse than mint a guessable approval credential.
func randomNonce() string {
	b := make([]byte, 18)
	if _, err := rand.Read(b); err != nil {
		panic("lattice: crypto/rand unavailable for approval nonce: " + err.Error())
	}
	return base64.RawURLEncoding.EncodeToString(b)
}

// handleSessionIdle reacts to a claude session's quiet↔active edge (reported by
// the agent's idle-watcher). Every edge is audited; an Idle=true edge for an
// opted-in session also arms a phone-approval and pushes to ntfy, while a resume
// (Idle=false) disarms it.
func (h *Hub) handleSessionIdle(agentID string, p proto.SessionIdlePayload) {
	now := time.Now()
	event := "session_active"
	if p.Idle {
		event = "session_idle"
	}
	if err := h.store.LogAudit(p.SessionID, agentID, event, "", "", now); err != nil {
		log.Printf("audit: %s log failed: %v", event, err)
	}
	if !p.Idle {
		h.approvals.dropForSession(p.SessionID) // output resumed — stale approval
		return
	}
	// D: the idle edge is the fallback (no-hooks) analogue of the Stop hook — claude
	// just went quiet, so it may have printed a PR URL. Detect off the transcript.
	goSafe("detectPR:"+p.SessionID, func() { h.detectPRForSession(p.SessionID, now) })
	// Session naming (I, v0.1.5): the first quiet edge means the model has answered
	// the first user message, so the first turn is now on disk — derive a title for
	// a still-untitled session. Independent of NotifyOnIdle (every fresh claude
	// session gets named) and run off the read loop (it round-trips to the agent).
	goSafe("autoName:"+p.SessionID, func() { h.maybeAutoName(agentID, p.SessionID) })

	rec, ok, err := h.store.GetSession(p.SessionID)
	if err != nil || !ok || !rec.NotifyOnIdle {
		return
	}
	h.notifyWaiting(rec, agentID, now)
}

// onSessionExit audits a finished session and, for an opted-in fire-and-forget run
// that died on its own (not a close the hub asked for), pushes a "finished" ping.
func (h *Hub) onSessionExit(agentID, sessionID string) {
	now := time.Now()
	h.approvals.dropForSession(sessionID)
	h.hooks.drop(sessionID) // hook token dies with the session
	if h.autoNamer != nil {
		h.autoNamer.forget(sessionID) // session ended — drop its auto-name state
	}
	expected := h.approvals.takeExpected(sessionID)
	rec, ok, err := h.store.GetSession(sessionID)
	if err != nil || !ok {
		return
	}
	if err := h.store.LogAudit(sessionID, agentID, "session_exit", "", "", now); err != nil {
		log.Printf("audit: session_exit log failed: %v", err)
	}
	if expected || !rec.NotifyOnIdle || !notifyEnabled() {
		return
	}
	msg := ntfyMessage{
		Title:    h.sessionLabel(rec) + " — finished",
		Message:  "Claude on " + h.agentDisplayName(agentID) + " ended its session.",
		Priority: 3,
		Tags:     []string{"checkered_flag"},
	}
	if h.hubURL != "" {
		msg.Click = h.hubURL + "/?session=" + url.QueryEscape(sessionID)
	}
	h.notify(msg)
}

// notifyWaiting arms an approval (when a canonical hub URL is configured) and
// pushes the "Claude is waiting" notification with Approve / Deny / Open buttons.
// Without a hub URL it still sends a plain heads-up — there's just no link the
// phone could resolve for the action buttons.
func (h *Hub) notifyWaiting(rec SessionRecord, agentID string, now time.Time) {
	if !notifyEnabled() {
		return
	}
	msg := ntfyMessage{
		Title:    h.sessionLabel(rec) + " — waiting",
		Message:  "Claude on " + h.agentDisplayName(agentID) + " is waiting for you.",
		Priority: 4,
		Tags:     []string{"hourglass_flowing_sand"},
	}
	if h.hubURL != "" {
		nonce := h.approvals.mint(rec.ID, agentID, now)
		open := h.hubURL + "/?session=" + url.QueryEscape(rec.ID)
		msg.Click = open
		msg.Actions = []ntfyAction{
			{Action: "http", Label: "Approve", URL: h.hubURL + "/api/approvals/" + nonce + "?d=y", Method: "POST", Clear: true},
			{Action: "http", Label: "Deny", URL: h.hubURL + "/api/approvals/" + nonce + "?d=n", Method: "POST", Clear: true},
			{Action: "view", Label: "Open", URL: open},
		}
	}
	h.notify(msg)
}

// sessionLabel is a short human name for a session in a push: its title, else the
// project folder, else a generic fallback.
func (h *Hub) sessionLabel(rec SessionRecord) string {
	if t := strings.TrimSpace(rec.Title); t != "" {
		return t
	}
	if rec.ProjectPath != "" {
		return filepath.Base(rec.ProjectPath)
	}
	return "Claude session"
}

// handleApproval resolves a phone approve/deny capability link. UNGATED on
// purpose: the unguessable single-use nonce in the path IS the credential (a
// capability URL), so it must NOT sit behind requireAuth — the phone tapping the
// ntfy button carries no admin token, and embedding the master token in a push on
// a public broker would be the real leak. ?d=y injects Enter (accept the
// highlighted prompt option); anything else (?d=n) injects Esc (cancel) into the
// waiting claude PTY via the owning agent.
func (h *Hub) handleApproval(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost && r.Method != http.MethodGet {
		w.Header().Set("Allow", "POST, GET")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	// Per-IP throttle (ungated capability endpoint). Legit taps arrive via the ntfy
	// server at a trickle; a flood (someone spraying the nonce space) is capped.
	if !requestIsLoopback(r) && !h.capLimiter.allow(clientIP(r)) {
		approvalPage(w, http.StatusTooManyRequests, "Too many requests — try again in a moment.")
		return
	}
	nonce := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/approvals/"), "/")
	if nonce == "" {
		approvalPage(w, http.StatusBadRequest, "Invalid approval link.")
		return
	}
	p, ok := h.approvals.consume(nonce, time.Now())
	if !ok {
		approvalPage(w, http.StatusGone, "This approval has expired or was already used.")
		return
	}

	approve := r.URL.Query().Get("d") != "n" // default to approve unless explicit deny
	keys, verb := "\r", "Approved"
	if !approve {
		keys, verb = "\x1b", "Denied"
	}

	ac, live := h.registry.getAgent(p.agentID)
	if !live {
		approvalPage(w, http.StatusConflict, "That machine is offline — couldn't deliver your response.")
		return
	}
	if err := ac.send(proto.TypeTermInput, proto.TermDataPayload{
		TermID: p.sessionID,
		Data:   base64.StdEncoding.EncodeToString([]byte(keys)),
	}); err != nil {
		approvalPage(w, http.StatusBadGateway, "Couldn't reach that machine — open the session to respond.")
		return
	}

	detail, _ := json.Marshal(map[string]string{"decision": strings.ToLower(verb)})
	if err := h.store.LogAudit(p.sessionID, p.agentID, "approval", verb, string(detail), time.Now()); err != nil {
		log.Printf("audit: approval log failed: %v", err)
	}
	h.broadcastSessions()
	approvalPage(w, http.StatusOK, verb+" — sent to "+h.agentDisplayName(p.agentID)+".")
}

// approvalPage renders a tiny mobile-friendly confirmation for the approve link.
// The ntfy http action fires in the background (it only needs a 2xx), so this
// matters mainly when the operator taps the raw link in a browser.
func approvalPage(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	body := `<!doctype html><html><head><meta name="viewport" content="width=device-width,initial-scale=1">` +
		`<title>Lattice</title><style>body{margin:0;height:100vh;display:flex;align-items:center;justify-content:center;` +
		`background:#0b0d10;color:#e6e9ef;font:16px/1.5 -apple-system,system-ui,sans-serif}` +
		`.c{max-width:22rem;padding:2rem;text-align:center}.c h1{font-size:.8rem;letter-spacing:.18em;color:#7fd6c2;margin:0 0 .75rem}` +
		`</style></head><body><div class="c"><h1>LATTICE</h1><p>` + html.EscapeString(msg) + `</p></div></body></html>`
	_, _ = w.Write([]byte(body))
}
