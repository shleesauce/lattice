package agent

import (
	"os"
	"strings"
)

// claudeChildEnv builds the environment for a spawned claude process, scrubbed of
// variables that would break subscription auth or confuse a nested launch. This
// matters because the agent itself may be started from INSIDE a Claude Code session
// (dogfooding, or the Tauri sidecar), inheriting an empty ANTHROPIC_API_KEY (which
// forces broken API-key auth → 401) plus CLAUDECODE/CLAUDE_CODE_* session markers.
// Removing ANTHROPIC_API_KEY forces the local Max subscription (OAuth) — which is
// both correct for the Claude tab and enforces the subscription-only cost rule. It
// stays load-bearing under D35: the interactive PTY launch reuses this scrubbed env.
func claudeChildEnv() []string {
	src := os.Environ()
	out := make([]string, 0, len(src))
	for _, kv := range src {
		key := kv
		if i := strings.IndexByte(kv, '='); i >= 0 {
			key = kv[:i]
		}
		switch {
		case key == "ANTHROPIC_API_KEY": // force subscription OAuth; never pay-per-token
			continue
		case key == "CLAUDECODE":
			continue
		case strings.HasPrefix(key, "CLAUDE_CODE_"): // nested-session markers (ENTRYPOINT, SESSION_ID, …)
			continue
		case key == "CLAUDE_AGENT_SDK_VERSION":
			continue
		}
		out = append(out, kv)
	}
	return out
}
