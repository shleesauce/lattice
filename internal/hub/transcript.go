package hub

import (
	"encoding/json"
	"net/http"
	"os"

	"github.com/shleesauce/lattice/internal/proto"
	"github.com/shleesauce/lattice/internal/transcript"
)

// Transcript view (F16 / fixes F15). Claude Code writes every session's full
// conversation to ~/.claude/projects/<encoded-cwd>/<claude-session-id>.jsonl on the
// machine that ran it. Two facts shape how the hub serves it:
//   - Those .jsonl files are deliberately NOT folder-synced (~/.claude/.stignore
//     "**/*.jsonl" — huge, machine-local, race-prone), and claude sessions often
//     can't run on the hub box (F14: a hub host may not be able to auth claude). So
//     the hub almost never has the file on its own disk — it must ask the owning
//     AGENT, which reads its own local copy and returns the parsed turns (same
//     round-trip as the file browser).
//   - When the owning agent is offline (an orphaned session), there's nothing to
//     fetch; we fall back to the hub's own disk (covers the rare hub-hosted session)
//     and otherwise return a clear "machine offline" reason instead of a blank tab.
//
// This turns an exited/archived/trashed/restored session — a blank tab today — into
// the saved conversation, word for word, with collapsible tool runs.

const emptyBlocksJSON = "[]"

// transcriptResponse is the JSON returned to the browser. Meta/Blocks are forwarded
// as raw JSON (agent-parsed transcript.Meta / []transcript.Block) so a large
// transcript isn't needlessly re-marshalled; Blocks is always a JSON array (never
// null) so the frontend can map over it unconditionally.
type transcriptResponse struct {
	SessionID string          `json:"sessionId"`
	Found     bool            `json:"found"`
	Reason    string          `json:"reason,omitempty"`
	Path      string          `json:"path,omitempty"`
	Meta      json.RawMessage `json:"meta,omitempty"`
	Blocks    json.RawMessage `json:"blocks"`
}

// handleTranscript answers GET /api/sessions/{id}/transcript. Network-gated like the
// other dashboard REST endpoints (the tailnet + hub gate access — D2/D3). A missing
// transcript is not an error: it returns 200 {found:false, reason} so the UI shows a
// graceful empty state (terminal/editor sessions never have one).
func (h *Hub) handleTranscript(w http.ResponseWriter, r *http.Request, id string) {
	rec, ok, err := h.store.GetSession(id)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	if !ok {
		http.NotFound(w, r)
		return
	}

	resp := transcriptResponse{SessionID: id, Blocks: json.RawMessage(emptyBlocksJSON)}

	if proto.SessionKind(rec.Kind) != proto.SessionClaude {
		resp.Reason = "only claude sessions have a transcript"
		writeJSON(w, http.StatusOK, resp)
		return
	}

	claudeID := rec.ClaudeSessionID
	if claudeID == "" {
		claudeID = rec.ID
	}

	// 1) Ask the owning agent (the only box with the file) when it's online.
	if _, online := h.registry.liveAgent(rec.AgentID); online {
		if got, ferr := h.fetchTranscriptFromAgent(rec.AgentID, rec.ID, claudeID); ferr == nil {
			if got.Found {
				resp.Found = true
				resp.Path = got.Path
				resp.Meta = got.Meta
				if len(got.Blocks) > 0 {
					resp.Blocks = got.Blocks
				}
				writeJSON(w, http.StatusOK, resp)
				return
			}
			// Agent answered "no file here" — fall through to the local fallback,
			// then a clear empty-state reason.
		} else {
			resp.Reason = "couldn't read the transcript from " + rec.AgentID + ": " + ferr.Error()
		}
	}

	// 2) Fallback: the hub's own disk (only hits for a session hosted on the hub box).
	if home, herr := os.UserHomeDir(); herr == nil {
		if blocks, meta, path, found := transcript.ParseFile(home, claudeID); found {
			resp.Found = true
			resp.Path = path
			if raw, mErr := json.Marshal(meta); mErr == nil {
				resp.Meta = raw
			}
			if raw, mErr := json.Marshal(blocks); mErr == nil && len(raw) > 0 {
				resp.Blocks = raw
			}
			writeJSON(w, http.StatusOK, resp)
			return
		}
	}

	// 3) Nothing to show. Explain why so the tab is never just blank.
	if resp.Reason == "" {
		if _, online := h.registry.liveAgent(rec.AgentID); !online {
			resp.Reason = "the machine that ran this session (" + rec.AgentID + ") is offline — its transcript lives on that box. Wake it in Fleet to view the history."
		} else {
			resp.Reason = "no transcript on disk for this session yet"
		}
	}
	writeJSON(w, http.StatusOK, resp)
}

// fetchTranscriptFromAgent round-trips a transcript_get to the owning agent and
// returns its parsed result. The agent reads its own ~/.claude/projects and answers
// with normalized blocks + meta (raw JSON we forward verbatim).
func (h *Hub) fetchTranscriptFromAgent(agentID, sessionID, claudeID string) (proto.TranscriptResultPayload, error) {
	reqID := newReqID()
	env, err := h.roundTrip(agentID, reqID, proto.TypeTranscriptGet, proto.TranscriptReqPayload{
		ReqID:           reqID,
		SessionID:       sessionID,
		ClaudeSessionID: claudeID,
	})
	if err != nil {
		return proto.TranscriptResultPayload{}, err
	}
	var res proto.TranscriptResultPayload
	if err := proto.As(env, &res); err != nil {
		return proto.TranscriptResultPayload{}, err
	}
	if res.Error != "" {
		return proto.TranscriptResultPayload{}, errFromAgent(res.Error)
	}
	return res, nil
}
