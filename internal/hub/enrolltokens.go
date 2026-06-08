package hub

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strings"
)

// enrollTokenView is the JSON shape for a per-machine enrollment token. Times are
// epoch SECONDS (0 ⇒ unset): createdAt is always set; revokedAt is 0 until the
// token is revoked; lastUsedAt is 0 until an agent enrolls with it.
type enrollTokenView struct {
	Token      string `json:"token"` // full token: this surface is admin-auth-gated
	Label      string `json:"label"`
	CreatedAt  int64  `json:"createdAt"`
	RevokedAt  int64  `json:"revokedAt"`
	LastUsedAt int64  `json:"lastUsedAt"`
	AgentID    string `json:"agentId"`
}

func toEnrollTokenView(rec EnrollTokenRecord) enrollTokenView {
	v := enrollTokenView{Token: rec.Token, Label: rec.Label, AgentID: rec.AgentID}
	if !rec.CreatedAt.IsZero() {
		v.CreatedAt = rec.CreatedAt.Unix()
	}
	if !rec.RevokedAt.IsZero() {
		v.RevokedAt = rec.RevokedAt.Unix()
	}
	if !rec.LastUsedAt.IsZero() {
		v.LastUsedAt = rec.LastUsedAt.Unix()
	}
	return v
}

// handleEnrollTokens lists (GET) or mints (POST) per-machine enrollment tokens.
// Both are admin-auth-gated (registered behind requireAuth) — these are operator
// ops, and the full token is returned in the clear because the operator must hand
// it to a new machine.
func (h *Hub) handleEnrollTokens(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		recs, err := h.store.ListEnrollTokens()
		if err != nil {
			log.Printf("enroll tokens: list failed: %v", err)
			http.Error(w, "failed to list tokens", http.StatusInternalServerError)
			return
		}
		out := make([]enrollTokenView, 0, len(recs))
		for _, rec := range recs {
			out = append(out, toEnrollTokenView(rec))
		}
		writeJSON(w, http.StatusOK, map[string]any{"tokens": out})

	case http.MethodPost:
		var body struct {
			Label string `json:"label"`
		}
		if r.Body != nil {
			// An empty body is fine (label optional); only reject malformed JSON.
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil && err.Error() != "EOF" {
				http.Error(w, "invalid json body", http.StatusBadRequest)
				return
			}
		}
		label := strings.TrimSpace(body.Label)
		if len(label) > 40 {
			http.Error(w, "label must be <= 40 characters", http.StatusBadRequest)
			return
		}
		token, err := randomEnrollToken()
		if err != nil {
			log.Printf("enroll tokens: token mint failed: %v", err)
			http.Error(w, "failed to create token", http.StatusInternalServerError)
			return
		}
		if err := h.store.CreateEnrollToken(token, label); err != nil {
			log.Printf("enroll tokens: create failed: %v", err)
			http.Error(w, "failed to create token", http.StatusInternalServerError)
			return
		}
		unix, windows := enrollOneLiners(h.canonicalHubURL(r), token)
		tsUnix, tsWin := tailscaleSetupOneLiners()
		log.Printf("enroll token minted: label=%q", label)
		writeJSON(w, http.StatusCreated, map[string]any{
			"token":            token,
			"label":            label,
			"unix":             unix,
			"windows":          windows,
			"tailscaleUnix":    tsUnix,
			"tailscaleWindows": tsWin,
		})

	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// handleEnrollTokenItem handles the /api/enroll/tokens/{token}/revoke subpath. It
// is registered on the /api/enroll/tokens/ PREFIX (distinct mux pattern from
// /api/enroll/tokens and /api/enroll), and parses {token} + the /revoke suffix.
func (h *Hub) handleEnrollTokenItem(w http.ResponseWriter, r *http.Request) {
	rest := strings.TrimPrefix(r.URL.Path, "/api/enroll/tokens/")
	token, action, ok := strings.Cut(rest, "/")
	if !ok || token == "" || action != "revoke" {
		http.NotFound(w, r)
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	// The MASTER token is never revocable — it is the trusted root every agent
	// enrolls with (and isn't even stored in enroll_tokens). Revoking it would
	// either silently no-op (it's absent from the table) or, worse, suggest it
	// can be invalidated; reject it explicitly so the invariant is unmistakable.
	// This is an equality guard on an admin-gated route, so constant-time isn't
	// required here.
	if h.isMasterToken(token) {
		http.Error(w, "the master enrollment token cannot be revoked", http.StatusBadRequest)
		return
	}
	if err := h.store.RevokeEnrollToken(token); err != nil {
		if errors.Is(err, errNoEnrollToken) {
			http.Error(w, "no such token", http.StatusNotFound)
			return
		}
		log.Printf("enroll tokens: revoke failed: %v", err)
		http.Error(w, "failed to revoke token", http.StatusInternalServerError)
		return
	}
	log.Printf("enroll token revoked: %s", tokenHint(token))
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// randomEnrollToken returns a 16-hex-char per-machine enrollment token (8 random
// bytes / 64 bits). It FAILS LOUDLY if crypto/rand is unavailable rather than
// emitting a predictable time-seeded token: a per-machine token is a credential, so
// a guessable one is exactly the failure to avoid, and rand.Read never fails on
// supported platforms. The caller surfaces the error as a 500 instead of minting.
func randomEnrollToken() (string, error) {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("hub: generate enroll token: %w", err)
	}
	return hex.EncodeToString(b), nil
}
