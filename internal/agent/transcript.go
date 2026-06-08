package agent

import (
	"context"
	"encoding/json"
	"os"

	"github.com/shleesauce/lattice/internal/proto"
	"github.com/shleesauce/lattice/internal/transcript"
)

// getTranscript serves a session's saved Claude transcript from THIS machine's
// disk (F16). Claude Code writes ~/.claude/projects/<encoded-cwd>/<session-id>.jsonl
// locally and those files are deliberately NOT folder-synced, so the owning agent
// is the only box that can read them — the hub round-trips here. We parse on the
// agent (not ship raw 8-MB JSONL over the WS) so the payload is proportional to
// real content, capped by the transcript package.
func getTranscript(ctx context.Context, p proto.TranscriptReqPayload, outbound chan<- []byte) {
	result := proto.TranscriptResultPayload{ReqID: p.ReqID}

	id := p.ClaudeSessionID
	if id == "" {
		id = p.SessionID
	}

	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		result.Error = "home dir unavailable"
		sendFrame(ctx, outbound, proto.TypeTranscriptResult, result)
		return
	}

	blocks, meta, path, found := transcript.ParseFile(home, id)
	if !found {
		// Not an error: this box simply has no transcript for that id (terminal
		// session, or the file hasn't been written yet).
		sendFrame(ctx, outbound, proto.TypeTranscriptResult, result)
		return
	}
	result.Found = true
	result.Path = path
	if raw, mErr := json.Marshal(blocks); mErr == nil {
		result.Blocks = raw
	}
	if raw, mErr := json.Marshal(meta); mErr == nil {
		result.Meta = raw
	}
	sendFrame(ctx, outbound, proto.TypeTranscriptResult, result)
}
