package hub

import (
	"encoding/json"
	"log"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/shleesauce/lattice/internal/proto"
	"github.com/shleesauce/lattice/internal/transcript"
)

// Session auto-naming (I — session naming, v0.1.5).
//
// Claude Code titles a session from a short summary of the first user message; we
// do the same, but with a FREE heuristic — truncate + clean the first user turn of
// the synced transcript. NEVER an LLM call (D35 draws the billing line at headless
// `claude -p`/SDK; an auto-title must not metered-bill). The trigger is the first
// `session_idle` edge for a fresh claude session (handleSessionIdle): by the time
// the model has gone quiet, the first user turn is on disk in the agent's jsonl,
// which the hub already fetches over the existing transcript round-trip.
//
// A manual rename ALWAYS wins: handleUpdateSession marks the session title-locked,
// and the auto-namer both skips locked sessions and only writes when the title is
// still blank — so a user-set name is never clobbered, even on a later idle edge.

// maxTitleWords / maxTitleLen bound a derived title to Claude-Code-ish length.
const (
	maxTitleWords = 5
	minTitleWords = 1
	maxTitleLen   = 48
)

// autoNamer tracks per-session auto-name state so a manual rename always wins and
// concurrent idle edges don't double-derive. All access is mutex-guarded; the maps
// are tiny (one entry per live untitled/just-named session) and swept on session
// end, so they don't grow unbounded over a long hub uptime.
type autoNamer struct {
	mu       sync.Mutex
	locked   map[string]bool // sessionId → user manually named it (auto-namer must skip)
	inflight map[string]bool // sessionId → a derivation is already running
}

func newAutoNamer() *autoNamer {
	return &autoNamer{
		locked:   make(map[string]bool),
		inflight: make(map[string]bool),
	}
}

// lock marks a session as user-named so the auto-namer never overwrites it.
func (a *autoNamer) lock(sessionID string) {
	a.mu.Lock()
	a.locked[sessionID] = true
	a.mu.Unlock()
}

func (a *autoNamer) isLocked(sessionID string) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.locked[sessionID]
}

// begin claims the right to derive a title for a session; returns false if it's
// already locked (user-named) or a derivation is in flight.
func (a *autoNamer) begin(sessionID string) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.locked[sessionID] || a.inflight[sessionID] {
		return false
	}
	a.inflight[sessionID] = true
	return true
}

func (a *autoNamer) done(sessionID string) {
	a.mu.Lock()
	delete(a.inflight, sessionID)
	a.mu.Unlock()
}

// forget drops all state for a session that ended, so the maps don't leak.
func (a *autoNamer) forget(sessionID string) {
	a.mu.Lock()
	delete(a.locked, sessionID)
	delete(a.inflight, sessionID)
	a.mu.Unlock()
}

// markTitleLocked records that the operator manually named a session, so the
// auto-namer never overwrites it. Called from the rename handler.
func (h *Hub) markTitleLocked(sessionID string) {
	if h.autoNamer != nil {
		h.autoNamer.lock(sessionID)
	}
}

// maybeAutoName derives and persists a short title for a fresh, still-untitled
// claude session from its first user message. Safe to call on every idle edge: it
// no-ops once the session has a title (auto or manual) or is being named. It runs
// the transcript round-trip in the CALLER's goroutine, so handleSessionIdle invokes
// it via `go` to avoid blocking the agent read loop.
func (h *Hub) maybeAutoName(agentID, sessionID string) {
	if h.autoNamer == nil {
		return
	}
	rec, ok, err := h.store.GetSession(sessionID)
	if err != nil || !ok {
		return
	}
	// Only claude sessions get a derived title; an already-titled (auto OR manual)
	// session is left alone. Locked = user named it explicitly → never touch.
	if proto.SessionKind(rec.Kind) != proto.SessionClaude {
		return
	}
	if strings.TrimSpace(rec.Title) != "" || h.autoNamer.isLocked(sessionID) {
		return
	}
	if !h.autoNamer.begin(sessionID) {
		return
	}
	defer h.autoNamer.done(sessionID)

	claudeID := rec.ClaudeSessionID
	if claudeID == "" {
		claudeID = rec.ID
	}

	// Fetch the parsed transcript from the owning agent (the only box with the
	// jsonl). Offline agent ⇒ nothing to derive from; try again on the next edge.
	if _, online := h.registry.liveAgent(agentID); !online {
		return
	}
	res, ferr := h.fetchTranscriptFromAgent(agentID, sessionID, claudeID)
	if ferr != nil || !res.Found || len(res.Blocks) == 0 {
		return
	}
	var blocks []transcript.Block
	if err := json.Unmarshal(res.Blocks, &blocks); err != nil || len(blocks) == 0 {
		return
	}

	first := firstUserText(blocks)
	title := deriveTitle(first)
	if title == "" {
		return
	}

	// Re-check under the lock right before writing: a manual rename may have landed
	// during the round-trip — it must win.
	if h.autoNamer.isLocked(sessionID) {
		return
	}
	if cur, ok, _ := h.store.GetSession(sessionID); ok && strings.TrimSpace(cur.Title) != "" {
		return // got named some other way mid-flight
	}
	if err := h.store.SetSessionTitle(sessionID, title, time.Now()); err != nil {
		log.Printf("autoname: set title for %s failed: %v", sessionID, err)
		return
	}
	h.broadcastSessions()
}

// deriveTitle turns the first user message into a 1–5 word title. FREE heuristic:
// strip noise (slash-commands, command/system tags, code fences, markdown), grab
// the first handful of meaningful words, Title-Case the lead, clamp the length.
func deriveTitle(msg string) string {
	s := cleanForTitle(msg)
	if s == "" {
		return ""
	}
	words := strings.Fields(s)
	// Drop low-signal stop-words (articles/pronouns/politeness) anywhere in the
	// phrase so "please fix the login bug" → "Fix Login Bug", Claude-Code style.
	// Keep at least one word so an all-stopword message still yields something.
	words = dropStopWords(words)
	if len(words) == 0 {
		return ""
	}
	if len(words) > maxTitleWords {
		words = words[:maxTitleWords]
	}
	_ = minTitleWords
	for i, w := range words {
		words[i] = titleWord(w)
	}
	return clampTitle(strings.Join(words, " "))
}

// firstUserText returns the text of the first genuine user turn in a parsed
// transcript, skipping tool_result blocks (Claude Code records command output and
// injected context as user-role tool_results — not what the human typed).
func firstUserText(blocks []transcript.Block) string {
	for _, b := range blocks {
		if b.Role != "user" || b.Kind != "text" {
			continue
		}
		t := strings.TrimSpace(b.Text)
		if t == "" {
			continue
		}
		return t
	}
	return ""
}

// cleanForTitle normalizes a raw first message into a flat, noise-free phrase:
// strips Claude Code's <command-*>/<system-*> XML wrappers and slash-command lines,
// drops code fences/inline code/markdown emphasis, and collapses whitespace.
func cleanForTitle(s string) string {
	s = strings.TrimSpace(s)
	// A bare slash-command ("/clear", "/model …") is meta, not a task — skip it so
	// the title comes from a real instruction.
	if strings.HasPrefix(s, "/") {
		if nl := strings.IndexByte(s, '\n'); nl >= 0 {
			s = strings.TrimSpace(s[nl+1:])
		} else {
			return ""
		}
	}
	// Claude Code wraps injected context in <command-name>/<command-message>/
	// <system-reminder> etc. Strip any angle-bracket tag wholesale.
	s = stripAngleTags(s)
	// Drop fenced code blocks entirely (a title from ```go … ``` is noise).
	s = stripFencedCode(s)
	// Use only the first non-empty line — the gist is almost always the opener.
	for _, ln := range strings.Split(s, "\n") {
		ln = strings.TrimSpace(ln)
		if ln != "" {
			s = ln
			break
		}
	}
	// Strip markdown emphasis / inline-code / list markers and odd punctuation.
	s = strings.Map(func(r rune) rune {
		switch r {
		case '`', '*', '_', '#', '>', '"', '\'':
			return -1
		}
		return r
	}, s)
	// Collapse runs of whitespace.
	return strings.Join(strings.Fields(s), " ")
}

// stripAngleTags removes <…> XML/HTML-ish tags (Claude Code's command/system
// wrappers) but keeps their inner text, so "<command-name>fix</command-name> the
// build" survives as "fix the build".
func stripAngleTags(s string) string {
	var b strings.Builder
	depth := 0
	for _, r := range s {
		switch r {
		case '<':
			depth++
		case '>':
			if depth > 0 {
				depth--
			}
		default:
			if depth == 0 {
				b.WriteRune(r)
			}
		}
	}
	return b.String()
}

// stripFencedCode drops everything between ``` fences.
func stripFencedCode(s string) string {
	for {
		i := strings.Index(s, "```")
		if i < 0 {
			return s
		}
		j := strings.Index(s[i+3:], "```")
		if j < 0 {
			return s[:i] // unterminated fence — drop the rest
		}
		s = s[:i] + " " + s[i+3+j+3:]
	}
}

// fillerWords are low-signal openers dropped from the front of a title so it leads
// with the verb/noun a human would skim for.
var fillerWords = map[string]bool{
	"please": true, "can": true, "could": true, "would": true, "you": true,
	"hey": true, "hi": true, "ok": true, "okay": true, "so": true, "now": true,
	"lets": true, "let": true, "i": true, "we": true, "the": true, "a": true,
	"an": true, "just": true, "want": true, "need": true, "to": true, "help": true,
	"me": true, "my": true,
}

// dropStopWords removes filler/stop-words anywhere in the phrase while preserving
// order. If every word is a stop-word, the original list is returned so a title is
// still produced (the last-resort fallback).
func dropStopWords(words []string) []string {
	out := make([]string, 0, len(words))
	for _, w := range words {
		key := strings.ToLower(strings.TrimFunc(w, func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) }))
		if fillerWords[key] {
			continue
		}
		out = append(out, w)
	}
	if len(out) == 0 {
		return words
	}
	return out
}

// titleWord strips edge punctuation (so "this:" → "This", "(auth)" → "Auth") then
// upper-cases the first letter, leaving the interior as typed so acronyms /
// camelCase identifiers (useLiveResource, API) aren't mangled.
func titleWord(w string) string {
	w = strings.TrimFunc(w, func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) })
	if w == "" {
		return w
	}
	r := []rune(w)
	r[0] = unicode.ToUpper(r[0])
	return string(r)
}

// clampTitle trims and length-limits any title (manual or derived) so one giant
// paste can't become a multi-KB row value. Word-boundary aware.
func clampTitle(s string) string {
	s = strings.Join(strings.Fields(strings.TrimSpace(s)), " ")
	if len(s) <= maxTitleLen {
		return s
	}
	cut := s[:maxTitleLen]
	if i := strings.LastIndexByte(cut, ' '); i > 0 {
		cut = cut[:i]
	}
	return strings.TrimSpace(cut)
}
