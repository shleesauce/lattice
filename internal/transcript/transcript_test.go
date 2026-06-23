package transcript

import (
	"os"
	"strings"
	"testing"
)

// A representative slice of a real Claude Code JSONL transcript: noise lines
// (queue-operation/last-prompt/system/attachment + an isMeta user msg) that must
// be skipped, a typed user prompt (string content), an assistant turn with
// thinking+text+tool_use and usage, and user tool_results (string and array form).
const sampleJSONL = `{"type":"queue-operation","operation":"enqueue","content":"x"}
{"type":"last-prompt","content":"x"}
{"type":"system","subtype":"stop_hook_summary","content":null}
{"type":"user","isMeta":true,"message":{"role":"user","content":"injected caveat"}}
{"type":"user","timestamp":"2026-06-03T10:00:00.000Z","message":{"role":"user","content":"hello build the thing"}}
{"type":"assistant","timestamp":"2026-06-03T10:00:01.000Z","message":{"role":"assistant","model":"claude-opus-4-8","usage":{"input_tokens":100,"output_tokens":20,"cache_read_input_tokens":5,"cache_creation_input_tokens":7},"content":[{"type":"thinking","text":"let me think"},{"type":"text","text":"On it."},{"type":"tool_use","name":"Bash","id":"toolu_1","input":{"command":"ls"}}]}}
{"type":"user","timestamp":"2026-06-03T10:00:02.000Z","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"toolu_1","content":"file1\nfile2"}]}}
{"type":"assistant","timestamp":"2026-06-03T10:00:03.000Z","message":{"role":"assistant","model":"claude-opus-4-8","usage":{"input_tokens":200,"output_tokens":30},"content":[{"type":"tool_use","name":"Read","id":"toolu_2","input":{"file_path":"/x"}}]}}
{"type":"user","timestamp":"2026-06-03T10:00:04.000Z","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"toolu_2","is_error":true,"content":[{"type":"text","text":"boom"},{"type":"image"}]}]}}
not even json
{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":""}]}}`

func TestParse(t *testing.T) {
	blocks, meta := Parse(strings.NewReader(sampleJSONL))

	// 0 user text, 1 thinking, 2 assistant text, 3 tool_use(Bash),
	// 4 tool_result(toolu_1), 5 tool_use(Read), 6 tool_result(toolu_2 error)
	if len(blocks) != 7 {
		for i, b := range blocks {
			t.Logf("block %d: role=%s kind=%s tool=%s text=%q", i, b.Role, b.Kind, b.ToolName, b.Text)
		}
		t.Fatalf("expected 7 blocks, got %d", len(blocks))
	}
	if blocks[0].Role != "user" || blocks[0].Kind != "text" || blocks[0].Text != "hello build the thing" {
		t.Errorf("block0 wrong: %+v", blocks[0])
	}
	if blocks[1].Kind != "thinking" || blocks[1].Text != "let me think" {
		t.Errorf("block1 (thinking) wrong: %+v", blocks[1])
	}
	if blocks[2].Kind != "text" || blocks[2].Text != "On it." {
		t.Errorf("block2 (text) wrong: %+v", blocks[2])
	}
	if blocks[3].Kind != "tool_use" || blocks[3].ToolName != "Bash" || blocks[3].ToolUseID != "toolu_1" {
		t.Errorf("block3 (tool_use) wrong: %+v", blocks[3])
	}
	if !strings.Contains(string(blocks[3].ToolInput), "\"command\"") {
		t.Errorf("block3 tool input missing: %s", blocks[3].ToolInput)
	}
	if blocks[4].Kind != "tool_result" || blocks[4].ToolUseID != "toolu_1" || blocks[4].Text != "file1\nfile2" {
		t.Errorf("block4 (tool_result) wrong: %+v", blocks[4])
	}
	if blocks[6].Kind != "tool_result" || !blocks[6].IsError || !strings.Contains(blocks[6].Text, "boom") || !strings.Contains(blocks[6].Text, "[image]") {
		t.Errorf("block6 (error tool_result) wrong: %+v", blocks[6])
	}
	for i, b := range blocks {
		if b.Seq != i {
			t.Errorf("block %d has seq %d", i, b.Seq)
		}
	}

	// Three assistant lines (the trailing empty-text one still counts as an API
	// turn; it carries no usage so it doesn't move the token totals).
	if meta.MessageCount != 3 {
		t.Errorf("messageCount = %d, want 3", meta.MessageCount)
	}
	if meta.InputTokens != 300 || meta.OutputTokens != 50 || meta.CacheReadTokens != 5 || meta.CacheCreationTokens != 7 {
		t.Errorf("token meta wrong: %+v", meta)
	}
	if meta.Model != "claude-opus-4-8" {
		t.Errorf("model = %q", meta.Model)
	}
	if meta.FirstAt != "2026-06-03T10:00:00.000Z" || meta.LastAt != "2026-06-03T10:00:04.000Z" {
		t.Errorf("timeline wrong: first=%q last=%q", meta.FirstAt, meta.LastAt)
	}
}

// A single tool_result line far exceeding the bufio default 64KB must parse whole
// (regression for the long-line reader) and clip to MaxBlockText.
func TestParseLongLine(t *testing.T) {
	big := strings.Repeat("A", MaxBlockText+50_000)
	line := `{"type":"user","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"t","content":"` + big + `"}]}}`
	blocks, _ := Parse(strings.NewReader(line))
	if len(blocks) != 1 {
		t.Fatalf("expected 1 block, got %d", len(blocks))
	}
	if !blocks[0].Truncated {
		t.Errorf("expected truncated flag")
	}
	if len(blocks[0].Text) > MaxBlockText+64 {
		t.Errorf("text not clipped: len=%d", len(blocks[0].Text))
	}
}

// Locate must reject ids that would escape the projects root, and find a real file
// laid out as ~/.claude/projects/<dir>/<id>.jsonl.
func TestLocate(t *testing.T) {
	home := t.TempDir()
	dir := home + "/.claude/projects/-Users-x-proj"
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	id := "abc123"
	if err := os.WriteFile(dir+"/"+id+".jsonl", []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	if p, ok := Locate(home, id); !ok || p == "" {
		t.Errorf("expected to locate %s, got ok=%v p=%q", id, ok, p)
	}
	if _, ok := Locate(home, "../../etc/passwd"); ok {
		t.Errorf("path-traversal id must not resolve")
	}
	if _, ok := Locate(home, "missing"); ok {
		t.Errorf("missing id must not resolve")
	}
}
