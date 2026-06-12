package hub

import (
	"crypto/subtle"
	"encoding/json"
	"log"
	"net/http"
	"sync"
	"time"
)

// Claude Code hooks → precise session state (C). A Lattice-launched claude session
// runs with `--settings <static lattice hooks file>` (per-invocation, zero
// ~/.claude footprint — D-scope decision). The hooks curl-POST to this hub the
// instant claude finishes a turn (Stop), starts waiting on a permission prompt
// (Notification matcher permission_prompt), or ends (SessionEnd). This is a precise
// signal that REPLACES the coarse 45s PTY-quiet idle heuristic for fire-and-forget:
// a Stop or a permission prompt is an exact "needs you" edge, not a guess.
//
// The endpoint is UNGATED by master auth on purpose, exactly like the approval
// capability links: the per-session HookToken in the body IS the credential. The
// hub mints it at create time, ships it to the agent in SessionCreatePayload, and
// the agent injects it into the claude child env so the hook script can present it.
// Embedding the master token in a hook that shells out on every turn would be the
// real leak.

// hookTokenTTL bounds how long a session's hook token stays registered after the
// session ends — long enough for a trailing SessionEnd to land, short enough that a
// leaked token is dead soon after. Tokens are also dropped explicitly on exit.
const hookTokenTTL = 12 * time.Hour

// hookStore maps a live session to its hook capability token. In-memory only: a hub
// restart drops them (the agent re-registers live sessions, but their in-flight
// hooks simply fall back to the idle heuristic until the next launch — safe).
type hookStore struct {
	mu     sync.Mutex
	tokens map[string]hookEntry // sessionId → token+mintedAt
}

type hookEntry struct {
	token    string
	mintedAt time.Time
}

func newHookStore() *hookStore {
	return &hookStore{tokens: make(map[string]hookEntry)}
}

// register binds a freshly-minted token to a session (called at create time).
func (s *hookStore) register(sessionID, token string, now time.Time) {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.tokens[sessionID] = hookEntry{token: token, mintedAt: now}
	s.mu.Unlock()
}

// valid reports whether a (sessionId, token) pair matches a registered hook token,
// in constant time so a wrong token can't be timed out.
func (s *hookStore) valid(sessionID, token string) bool {
	if s == nil {
		return false
	}
	s.mu.Lock()
	e, ok := s.tokens[sessionID]
	s.mu.Unlock()
	if !ok || token == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(e.token), []byte(token)) == 1
}

// drop removes a session's token (called on exit).
func (s *hookStore) drop(sessionID string) {
	if s == nil {
		return
	}
	s.mu.Lock()
	delete(s.tokens, sessionID)
	s.mu.Unlock()
}

// sweep expires stale tokens for sessions that never sent a clean SessionEnd.
func (s *hookStore) sweep(now time.Time) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for id, e := range s.tokens {
		if now.Sub(e.mintedAt) > hookTokenTTL {
			delete(s.tokens, id)
		}
	}
}

// hookStateBody is the JSON a Lattice hook script POSTs. `event` is the precise CC
// lifecycle edge; `token` is the per-session capability the hub minted. matcher
// distinguishes the Notification subtype (permission_prompt vs anything else).
type hookStateBody struct {
	SessionID string `json:"sessionId"`
	Token     string `json:"token"`
	Event     string `json:"event"`             // stop | notification | session_end
	Matcher   string `json:"matcher,omitempty"` // notification only: permission_prompt | …
}

// handleHookState receives a Claude Code hook callback and turns it into a precise
// session state edge. UNGATED by master auth (capability-token model — see file
// header). Always returns 200 quickly: the hook runs in claude's blocking path
// (default 600s timeout) and is wrapped in `timeout … || exit 0`, so a slow or
// rejected response must never wedge claude.
func (h *Hub) handleHookState(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	// Per-IP throttle (ungated endpoint). The co-located agent posts from loopback on
	// every turn, so exempt loopback; a remote flood is still capped. Return 200 (not
	// 429) so a throttled hook never looks like a failure that could wedge claude.
	if !requestIsLoopback(r) && !h.capLimiter.allow(clientIP(r)) {
		w.WriteHeader(http.StatusOK)
		return
	}
	var body hookStateBody
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10)).Decode(&body); err != nil {
		// Don't 4xx loudly — a malformed hook must not look like a failure to claude.
		w.WriteHeader(http.StatusOK)
		return
	}
	if body.SessionID == "" || !h.hooks.valid(body.SessionID, body.Token) {
		// Unknown/forged token: accept-and-ignore (200) so a probe learns nothing and
		// claude never blocks. Log at debug only.
		w.WriteHeader(http.StatusOK)
		return
	}

	now := time.Now()
	switch body.Event {
	case "notification":
		if body.Matcher == "permission_prompt" {
			// Claude is blocked on a permission gate — the highest-value "needs you"
			// edge. Mark awaiting-approval and fire the fire-and-forget push.
			h.onHookAwaiting(body.SessionID, now)
		}
		// Other notification matchers (idle_prompt etc.) are intentionally ignored —
		// Stop is the authoritative turn-done signal.
	case "stop":
		// Claude finished its turn and is waiting for the next instruction. Precise
		// "done / your move" edge.
		h.onHookStop(body.SessionID, now)
	case "session_end":
		// The session is ending. The agent's session_exit frame is the authoritative
		// teardown; this is just an early precise marker. Drop the token.
		h.onHookSessionEnd(body.SessionID, now)
	}
	w.WriteHeader(http.StatusOK)
}

// onHookStop records a turn-done edge: audit it + refresh the dashboard, but does
// NOT ping the phone. A turn finishing fires on EVERY interactive exchange, which
// made ntfy unusably noisy (dogfood BUG-005); the phone is reserved for the
// meaningful "needs you / done" edges — a permission gate (onHookAwaiting), the
// session ending (notifyExit), or a PR opening (notifyPROpened). The dashboard still
// reflects every turn live via broadcastSessions.
func (h *Hub) onHookStop(sessionID string, now time.Time) {
	rec, ok, err := h.store.GetSession(sessionID)
	if err != nil || !ok {
		return
	}
	if err := h.store.LogAudit(sessionID, rec.AgentID, "turn_done", "", "", now); err != nil {
		log.Printf("audit: turn_done log failed: %v", err)
	}
	h.broadcastSessions()
	// D: a finished turn is exactly when claude has just printed a PR URL (e.g. after
	// `gh pr create`). Enrich off the same transcript pipeline — own goroutine so it
	// never delays the hook.
	goSafe("detectPR:"+sessionID, func() { h.detectPRForSession(sessionID, now) })
}

// onHookAwaiting records an awaiting-permission edge and pings the phone (with the
// approve/deny capability buttons) for an opted-in session. This is the exact
// "claude is blocked on a permission prompt" signal ai-beacon surfaces.
func (h *Hub) onHookAwaiting(sessionID string, now time.Time) {
	rec, ok, err := h.store.GetSession(sessionID)
	if err != nil || !ok {
		return
	}
	detail, _ := json.Marshal(map[string]string{"state": "awaiting_approval"})
	if err := h.store.LogAudit(sessionID, rec.AgentID, "awaiting_approval", "", string(detail), now); err != nil {
		log.Printf("audit: awaiting_approval log failed: %v", err)
	}
	h.broadcastSessions()
	if rec.NotifyOnIdle {
		h.notifyWaiting(rec, rec.AgentID, now)
	}
}

// onHookSessionEnd is an early precise end marker: drop the hook token and disarm
// approvals. The agent's session_exit (handled in agentws) still does the
// authoritative status flip + "finished" ping, so we don't duplicate that here.
func (h *Hub) onHookSessionEnd(sessionID string, now time.Time) {
	h.hooks.drop(sessionID)
	h.approvals.dropForSession(sessionID)
	if err := h.store.LogAudit(sessionID, "", "session_end_hook", "", "", now); err != nil {
		log.Printf("audit: session_end_hook log failed: %v", err)
	}
}

// mintHookToken creates and registers a fresh per-session hook capability token.
// Reuses the same unguessable nonce generator as approvals.
func (h *Hub) mintHookToken(sessionID string, now time.Time) string {
	tok := randomNonce()
	h.hooks.register(sessionID, tok, now)
	return tok
}

// hooksEnabled reports whether the hub can wire CC hooks: it needs a canonical
// HubURL the on-agent hook script can curl back to. Without one, sessions launch
// without --settings and the hub keeps the PTY-quiet idle heuristic.
func (h *Hub) hooksEnabled() bool {
	return h.hubURL != ""
}
