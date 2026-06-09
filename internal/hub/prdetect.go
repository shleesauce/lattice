package hub

import (
	"encoding/json"
	"log"
	"net/url"
	"os"
	"regexp"
	"time"

	"github.com/shleesauce/lattice/internal/proto"
	"github.com/shleesauce/lattice/internal/transcript"
)

// PR detection (D, v0.1.5). An enricher off the C transcript pipeline: when a claude
// session finishes a turn (Stop hook), scan its synced transcript for a GitHub pull
// request URL — the line `gh pr create` prints, or a PR link claude reports. The
// FIRST one found is surfaced on the session card (prUrl) and fires exactly ONE
// "PR opened" ntfy push. Dedupe is structural: the URL is persisted on the session
// row, and detection is a no-op once it's set, so the push fires once per session
// even though Stop fires every turn.

// prURLRe matches a GitHub pull-request URL: https://github.com/<owner>/<repo>/pull/<n>.
// Anchored to the /pull/<digits> shape so an issues/commit/compare link doesn't
// match. Trailing path/query (e.g. /files) is tolerated but trimmed to the canonical
// PR URL.
var prURLRe = regexp.MustCompile(`https://github\.com/[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+/pull/\d+`)

// detectPRForSession scans a finished session's transcript for a PR URL and, on the
// first detection, persists it + fires one "PR opened" push. Best-effort and
// idempotent: returns early if the session already has a PR recorded, isn't a claude
// session, or has no readable transcript. Runs in its own goroutine off the Stop
// hook so it never delays the hook response.
func (h *Hub) detectPRForSession(sessionID string, now time.Time) {
	rec, ok, err := h.store.GetSession(sessionID)
	if err != nil || !ok {
		return
	}
	if proto.SessionKind(rec.Kind) != proto.SessionClaude {
		return
	}
	// Dedupe: already detected for this session → nothing to do.
	if rec.PRURL != "" {
		return
	}

	prURL := h.scanTranscriptForPR(rec)
	if prURL == "" {
		return
	}

	// Persist first (the structural dedupe). A racing second Stop that re-scans will
	// see the row set and bail, so the push below fires exactly once.
	set, serr := h.store.SetSessionPRURLIfEmpty(sessionID, prURL, now)
	if serr != nil {
		log.Printf("prdetect: persist %s failed: %v", sessionID, serr)
		return
	}
	if !set {
		return // another turn won the race; it already fired the push
	}

	detail, _ := json.Marshal(map[string]string{"prUrl": prURL})
	if err := h.store.LogAudit(sessionID, rec.AgentID, "pr_opened", "", string(detail), now); err != nil {
		log.Printf("audit: pr_opened log failed: %v", err)
	}
	h.broadcastSessions()
	h.notifyPROpened(rec, prURL)
}

// scanTranscriptForPR fetches the session transcript (owning agent, else hub disk)
// and returns the first GitHub PR URL in any block's text, or "".
func (h *Hub) scanTranscriptForPR(rec SessionRecord) string {
	claudeID := rec.ClaudeSessionID
	if claudeID == "" {
		claudeID = rec.ID
	}

	var blocks []transcript.Block
	if _, online := h.registry.liveAgent(rec.AgentID); online {
		if got, ferr := h.fetchTranscriptFromAgent(rec.AgentID, rec.ID, claudeID); ferr == nil && got.Found && len(got.Blocks) > 0 {
			_ = json.Unmarshal(got.Blocks, &blocks)
		}
	}
	if len(blocks) == 0 {
		if home, herr := os.UserHomeDir(); herr == nil {
			if bs, _, _, found := transcript.ParseFile(home, claudeID); found {
				blocks = bs
			}
		}
	}
	return firstPRURL(blocks)
}

// firstPRURL returns the first GitHub PR URL across the blocks' text (scanning
// newest-last so the most recent PR a long session opened wins). Exported-ish for
// testing as a pure function.
func firstPRURL(blocks []transcript.Block) string {
	for i := len(blocks) - 1; i >= 0; i-- {
		if m := prURLRe.FindString(blocks[i].Text); m != "" {
			return canonicalPRURL(m)
		}
	}
	return ""
}

// canonicalPRURL trims a matched PR URL to its canonical .../pull/<n> form (the
// regex already stops at the number, so this is just a safety re-match) and
// validates it parses as a URL.
func canonicalPRURL(raw string) string {
	m := prURLRe.FindString(raw)
	if m == "" {
		return ""
	}
	if _, err := url.Parse(m); err != nil {
		return ""
	}
	return m
}

// notifyPROpened pushes one "PR opened" notification (reusing the ntfy client), with
// a tap-through to the PR. Silent when notifications are disabled (empty topic).
func (h *Hub) notifyPROpened(rec SessionRecord, prURL string) {
	if !notifyEnabled() {
		return
	}
	msg := ntfyMessage{
		Title:    h.sessionLabel(rec) + " — PR opened",
		Message:  "Claude on " + prettyAgentName(rec.AgentID) + " opened a pull request.",
		Priority: 4,
		Tags:     []string{"twisted_rightwards_arrows"},
		Click:    prURL,
		Actions: []ntfyAction{
			{Action: "view", Label: "Open PR", URL: prURL},
		},
	}
	// Also offer a jump back into the session when a hub URL is configured.
	if h.hubURL != "" {
		msg.Actions = append(msg.Actions, ntfyAction{
			Action: "view", Label: "Session", URL: h.hubURL + "/?session=" + url.QueryEscape(rec.ID),
		})
	}
	h.notify(msg)
}
