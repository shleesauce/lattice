// Package transcript locates and parses Claude Code's on-disk session transcripts
// (the ~/.claude/projects/<encoded-cwd>/<session-id>.jsonl files) into a normalized
// turn list for Lattice's read-only transcript view (F16 / fixes F15).
//
// It is shared by the AGENT (which reads its OWN machine's transcript and returns
// the parsed result over the WS round-trip — transcripts are deliberately NOT
// Syncthing-synced, see ~/.claude/.stignore "**/*.jsonl", so each machine must
// serve its own) and by the HUB (a local-disk fallback for the rare session hosted
// on the hub box). Keeping locate+parse in one place guarantees both sides agree.
package transcript

import (
	"bufio"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// MaxBlockText clips one block's text so a single multi-MB tool_result (a giant
// build log) can't blow up the payload. The UI collapses tool blocks anyway.
const MaxBlockText = 200 << 10 // 200 KiB

// MaxBlocks caps the number of normalized blocks returned for one transcript, so a
// pathologically long session can't produce an unbounded response. The oldest turns
// are dropped first (the tail — most recent — is what the user opens to). When this
// trips, Meta.Truncated is set.
const MaxBlocks = 4000

// Block is one normalized content block. A single assistant message fans out into
// several blocks (thinking / text / one per tool_use); a user message into its text
// and/or tool_result blocks. Flat blocks let the UI collapse each tool run on its own.
type Block struct {
	Seq       int             `json:"seq"`
	Role      string          `json:"role"` // user | assistant
	Kind      string          `json:"kind"` // text | thinking | tool_use | tool_result | image
	Text      string          `json:"text,omitempty"`
	ToolName  string          `json:"toolName,omitempty"`
	ToolInput json.RawMessage `json:"toolInput,omitempty"`
	ToolUseID string          `json:"toolUseId,omitempty"`
	IsError   bool            `json:"isError,omitempty"`
	Truncated bool            `json:"truncated,omitempty"`
	Sidechain bool            `json:"sidechain,omitempty"`
	Timestamp string          `json:"timestamp,omitempty"`
}

// Meta is the aggregate token/timeline summary. Token counts are summed across
// every assistant API call (matching Claude Code's own informational cost meter).
type Meta struct {
	Model               string `json:"model,omitempty"`
	InputTokens         int    `json:"inputTokens"`
	OutputTokens        int    `json:"outputTokens"`
	CacheReadTokens     int    `json:"cacheReadTokens"`
	CacheCreationTokens int    `json:"cacheCreationTokens"`
	MessageCount        int    `json:"messageCount"`
	FirstAt             string `json:"firstAt,omitempty"`
	LastAt              string `json:"lastAt,omitempty"`
	Truncated           bool   `json:"truncated,omitempty"` // older turns dropped (MaxBlocks)
}

// Locate finds <claudeSessionID>.jsonl under home/.claude/projects. The session id
// is a globally-unique UUID, so a glob across every encoded-cwd dir reliably finds
// the one transcript regardless of how the cwd encoded (the per-machine home / D23
// rebase makes reconstructing the exact dir ambiguous). home is usually
// os.UserHomeDir(); passing it in keeps the function testable.
func Locate(home, claudeSessionID string) (string, bool) {
	id := filepath.Base(claudeSessionID) // never let an id escape the glob root
	if home == "" || id == "" || id == "." || strings.ContainsAny(id, `/\`) {
		return "", false
	}
	root := filepath.Join(home, ".claude", "projects")
	matches, err := filepath.Glob(filepath.Join(root, "*", id+".jsonl"))
	if err != nil || len(matches) == 0 {
		return "", false
	}
	// One file per id in practice; prefer the largest (most complete) on the off
	// chance a stray empty stub also matches.
	best, bestSize := "", int64(-1)
	for _, m := range matches {
		if info, statErr := os.Stat(m); statErr == nil && info.Size() > bestSize {
			best, bestSize = m, info.Size()
		}
	}
	if best == "" {
		best = matches[0]
	}
	return best, true
}

// ParseFile locates+opens+parses a session's transcript in one call. found is false
// when no .jsonl exists for the id on this machine.
func ParseFile(home, claudeSessionID string) (blocks []Block, meta Meta, path string, found bool) {
	p, ok := Locate(home, claudeSessionID)
	if !ok {
		return nil, Meta{}, "", false
	}
	f, err := os.Open(p)
	if err != nil {
		return nil, Meta{}, "", false
	}
	defer f.Close()
	blocks, meta = Parse(f)
	return blocks, meta, p, true
}

// --- JSONL line shapes (only the fields we read) ---

type rawLine struct {
	Type        string          `json:"type"` // user | assistant | system | queue-operation | …
	IsSidechain bool            `json:"isSidechain"`
	IsMeta      bool            `json:"isMeta"`
	Timestamp   string          `json:"timestamp"`
	Message     json.RawMessage `json:"message"`
}

type rawMessage struct {
	Role    string          `json:"role"`
	Model   string          `json:"model"`
	Content json.RawMessage `json:"content"` // string OR []rawBlock
	Usage   *rawUsage       `json:"usage"`
}

type rawUsage struct {
	InputTokens              int `json:"input_tokens"`
	OutputTokens             int `json:"output_tokens"`
	CacheReadInputTokens     int `json:"cache_read_input_tokens"`
	CacheCreationInputTokens int `json:"cache_creation_input_tokens"`
}

type rawBlock struct {
	Type      string          `json:"type"` // text | thinking | tool_use | tool_result | image
	Text      string          `json:"text"`
	Name      string          `json:"name"`        // tool_use
	ID        string          `json:"id"`          // tool_use
	Input     json.RawMessage `json:"input"`       // tool_use
	ToolUseID string          `json:"tool_use_id"` // tool_result
	IsError   bool            `json:"is_error"`    // tool_result
	Content   json.RawMessage `json:"content"`     // tool_result: string OR []rawBlock
}

// Parse streams a JSONL transcript into normalized blocks + aggregate meta. It
// tolerates malformed lines (skips them) and reads arbitrarily long lines (a single
// tool_result line can be hundreds of KB — past bufio.Scanner's 64KB cap).
func Parse(r io.Reader) ([]Block, Meta) {
	blocks := []Block{}
	var meta Meta
	seq := 0

	br := bufio.NewReaderSize(r, 1<<20)
	for {
		line, err := readLongLine(br)
		if len(line) > 0 {
			parseLine(line, &blocks, &meta, &seq)
		}
		if err != nil {
			break
		}
	}
	// Keep the most recent MaxBlocks (the tail) — that's what a reader opens to.
	if len(blocks) > MaxBlocks {
		blocks = blocks[len(blocks)-MaxBlocks:]
		meta.Truncated = true
	}
	return blocks, meta
}

func readLongLine(br *bufio.Reader) ([]byte, error) {
	var buf []byte
	for {
		chunk, err := br.ReadBytes('\n')
		buf = append(buf, chunk...)
		if err == bufio.ErrBufferFull {
			continue
		}
		return trimNL(buf), err
	}
}

func trimNL(b []byte) []byte {
	for len(b) > 0 && (b[len(b)-1] == '\n' || b[len(b)-1] == '\r') {
		b = b[:len(b)-1]
	}
	return b
}

func parseLine(line []byte, blocks *[]Block, meta *Meta, seq *int) {
	var rl rawLine
	if err := json.Unmarshal(line, &rl); err != nil {
		return
	}
	// Only conversation turns; skip queue-operation/last-prompt/attachment/system
	// noise and injected meta context.
	if rl.Type != "user" && rl.Type != "assistant" {
		return
	}
	if rl.IsMeta || len(rl.Message) == 0 {
		return
	}
	var msg rawMessage
	if err := json.Unmarshal(rl.Message, &msg); err != nil {
		return
	}
	role := msg.Role
	if role == "" {
		role = rl.Type
	}

	if rl.Type == "assistant" {
		if msg.Model != "" {
			meta.Model = msg.Model
		}
		if msg.Usage != nil {
			meta.InputTokens += msg.Usage.InputTokens
			meta.OutputTokens += msg.Usage.OutputTokens
			meta.CacheReadTokens += msg.Usage.CacheReadInputTokens
			meta.CacheCreationTokens += msg.Usage.CacheCreationInputTokens
		}
		meta.MessageCount++
	}
	if rl.Timestamp != "" {
		if meta.FirstAt == "" {
			meta.FirstAt = rl.Timestamp
		}
		meta.LastAt = rl.Timestamp
	}

	// content is either a plain string (a typed user prompt) or an array of blocks.
	var asString string
	if err := json.Unmarshal(msg.Content, &asString); err == nil {
		if strings.TrimSpace(asString) != "" {
			appendBlock(blocks, seq, Block{
				Role: role, Kind: "text", Text: clip(asString),
				Truncated: len(asString) > MaxBlockText, Sidechain: rl.IsSidechain, Timestamp: rl.Timestamp,
			})
		}
		return
	}

	var raw []rawBlock
	if err := json.Unmarshal(msg.Content, &raw); err != nil {
		return
	}
	for _, b := range raw {
		out := Block{Role: role, Sidechain: rl.IsSidechain, Timestamp: rl.Timestamp}
		switch b.Type {
		case "text", "thinking":
			if strings.TrimSpace(b.Text) == "" {
				continue
			}
			out.Kind = b.Type
			out.Text = clip(b.Text)
			out.Truncated = len(b.Text) > MaxBlockText
		case "tool_use":
			out.Kind = "tool_use"
			out.ToolName = b.Name
			out.ToolUseID = b.ID
			out.ToolInput = b.Input
		case "tool_result":
			out.Kind = "tool_result"
			out.ToolUseID = b.ToolUseID
			out.IsError = b.IsError
			txt := resultText(b.Content)
			out.Text = clip(txt)
			out.Truncated = len(txt) > MaxBlockText
		case "image":
			out.Kind = "image"
			out.Text = "[image]"
		default:
			continue
		}
		appendBlock(blocks, seq, out)
	}
}

func appendBlock(blocks *[]Block, seq *int, b Block) {
	b.Seq = *seq
	*seq++
	*blocks = append(*blocks, b)
}

// resultText flattens a tool_result's content (string, or an array of text/image
// blocks) into a single string for display.
func resultText(content json.RawMessage) string {
	if len(content) == 0 {
		return ""
	}
	var s string
	if err := json.Unmarshal(content, &s); err == nil {
		return s
	}
	var parts []rawBlock
	if err := json.Unmarshal(content, &parts); err != nil {
		return ""
	}
	var sb strings.Builder
	for _, p := range parts {
		switch p.Type {
		case "text":
			sb.WriteString(p.Text)
		case "image":
			sb.WriteString("[image]")
		}
	}
	return sb.String()
}

func clip(s string) string {
	if len(s) <= MaxBlockText {
		return s
	}
	return s[:MaxBlockText] + "\n…[truncated]"
}
