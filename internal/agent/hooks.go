package agent

import (
	"os"
	"path/filepath"
	"runtime"
	"sync"
)

// Claude Code hooks → precise Lattice session state (C, v0.1.5).
//
// A Lattice-launched claude session runs with `--settings <this file>` (verified
// on claude 2.1.137 — per-invocation, loads additional `hooks` for THAT launch
// only, zero ~/.claude footprint). The settings point three CC lifecycle hooks at a
// tiny script that curl-POSTs the precise edge to the hub:
//   - Stop                              → turn done / your move
//   - Notification (permission_prompt)  → blocked on a permission gate
//   - SessionEnd                        → session ending
//
// Per-session data (sessionId, hub URL, capability token) is injected into the
// claude child ENV (cmd.Env) — see hookEnv — so ONE static settings+script pair
// serves every session. The script is wrapped in `timeout … || exit 0` so a slow
// or rejected hub never wedges claude (hooks block claude up to 600s by default).
//
// SCOPE GUARANTEE: this writes ONLY under ~/.lattice/hooks and is passed via
// --settings. It NEVER reads or writes ~/.claude/settings.json (Dylan's hard rule).

var (
	hookSetupOnce sync.Once
	hookSetupErr  error
)

// hookSettingsPath returns the absolute path to the Lattice hook settings file,
// writing it (and the curl script) under ~/.lattice/hooks on first call. Returns ""
// if the files can't be written — the caller then launches WITHOUT --settings and
// the hub falls back to the PTY-quiet idle heuristic. Idempotent + concurrency-safe.
func hookSettingsPath() string {
	dir, err := hookDir()
	if err != nil {
		return ""
	}
	hookSetupOnce.Do(func() { hookSetupErr = writeHookFiles(dir) })
	if hookSetupErr != nil {
		return ""
	}
	return filepath.Join(dir, "settings.json")
}

// hookDir is ~/.lattice/hooks.
func hookDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".lattice", "hooks"), nil
}

// writeHookFiles (re)writes the settings JSON + the notify script. Always rewritten
// so a binary upgrade refreshes the contract. The script paths are absolute so
// claude resolves them regardless of cwd.
func writeHookFiles(dir string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}

	if runtime.GOOS == "windows" {
		scriptPath := filepath.Join(dir, "notify.cmd")
		if err := os.WriteFile(scriptPath, []byte(hookScriptWindows), 0o755); err != nil {
			return err
		}
		settings := hookSettingsJSON(scriptPath)
		return os.WriteFile(filepath.Join(dir, "settings.json"), []byte(settings), 0o644)
	}

	scriptPath := filepath.Join(dir, "notify.sh")
	if err := os.WriteFile(scriptPath, []byte(hookScriptUnix), 0o755); err != nil {
		return err
	}
	settings := hookSettingsJSON(scriptPath)
	return os.WriteFile(filepath.Join(dir, "settings.json"), []byte(settings), 0o644)
}

// hookEnv returns the per-session env additions for a claude launch with hooks
// wired: the hub URL, session id, and capability token the notify script reads.
// Empty slice when hooks aren't wired (no HubURL/HookToken in the create payload).
func hookEnv(hubURL, sessionID, token string) []string {
	if hubURL == "" || token == "" || sessionID == "" {
		return nil
	}
	return []string{
		"LATTICE_HUB_URL=" + hubURL,
		"LATTICE_SESSION_ID=" + sessionID,
		"LATTICE_HOOK_TOKEN=" + token,
	}
}

// hookSettingsJSON builds the --settings document wiring the three hooks to the
// notify script. The script's first arg is the event name; the matcher is encoded
// per-hook (Notification carries the prompt subtype on stdin, which the script
// forwards). A short per-hook timeout + the script's own `|| exit 0` guarantee a
// hook never blocks claude's turn.
func hookSettingsJSON(scriptPath string) string {
	// JSON-escape the path (Windows backslashes especially).
	p := jsonEscape(scriptPath)
	return `{
  "hooks": {
    "Stop": [
      { "hooks": [ { "type": "command", "command": "` + p + ` stop", "timeout": 5 } ] }
    ],
    "Notification": [
      { "matcher": "permission_prompt", "hooks": [ { "type": "command", "command": "` + p + ` notification permission_prompt", "timeout": 5 } ] }
    ],
    "SessionEnd": [
      { "hooks": [ { "type": "command", "command": "` + p + ` session_end", "timeout": 5 } ] }
    ]
  }
}
`
}

// jsonEscape escapes a string for embedding in a JSON string literal (quotes +
// backslashes — enough for filesystem paths).
func jsonEscape(s string) string {
	out := make([]rune, 0, len(s)+4)
	for _, r := range s {
		switch r {
		case '\\':
			out = append(out, '\\', '\\')
		case '"':
			out = append(out, '\\', '"')
		default:
			out = append(out, r)
		}
	}
	return string(out)
}

// hookScriptUnix posts the precise state edge to the hub. POSIX sh, no bashisms.
// $1 = event (stop|notification|session_end), $2 = matcher (notification only).
// `timeout` caps the curl; `|| exit 0` (and the leading guards) guarantee claude is
// never blocked or failed by a hook. Hooks read stdin (the CC event JSON) but we
// only need the event/matcher we passed as args + the env the agent injected.
const hookScriptUnix = `#!/bin/sh
# Lattice CC hook → precise session state. Never block/fail claude.
[ -n "$LATTICE_HUB_URL" ] || exit 0
[ -n "$LATTICE_HOOK_TOKEN" ] || exit 0
[ -n "$LATTICE_SESSION_ID" ] || exit 0
EVENT="$1"
MATCHER="$2"
BODY='{"sessionId":"'"$LATTICE_SESSION_ID"'","token":"'"$LATTICE_HOOK_TOKEN"'","event":"'"$EVENT"'","matcher":"'"$MATCHER"'"}'
# Prefer GNU/coreutils timeout when present; fall back to a bare curl with its own
# connect/max-time caps so the hook still can't hang.
if command -v timeout >/dev/null 2>&1; then
  timeout 4 curl -s -m 3 -X POST -H 'Content-Type: application/json' -d "$BODY" "$LATTICE_HUB_URL/api/hooks/state" >/dev/null 2>&1 || exit 0
else
  curl -s --connect-timeout 2 -m 3 -X POST -H 'Content-Type: application/json' -d "$BODY" "$LATTICE_HUB_URL/api/hooks/state" >/dev/null 2>&1 || exit 0
fi
exit 0
`

// hookScriptWindows is the cmd.exe equivalent. %1 = event, %2 = matcher. curl ships
// with modern Windows; the -m cap keeps it from hanging.
const hookScriptWindows = `@echo off
if "%LATTICE_HUB_URL%"=="" exit /b 0
if "%LATTICE_HOOK_TOKEN%"=="" exit /b 0
if "%LATTICE_SESSION_ID%"=="" exit /b 0
set "EVENT=%~1"
set "MATCHER=%~2"
set "BODY={\"sessionId\":\"%LATTICE_SESSION_ID%\",\"token\":\"%LATTICE_HOOK_TOKEN%\",\"event\":\"%EVENT%\",\"matcher\":\"%MATCHER%\"}"
curl -s --connect-timeout 2 -m 3 -X POST -H "Content-Type: application/json" -d "%BODY%" "%LATTICE_HUB_URL%/api/hooks/state" >NUL 2>&1
exit /b 0
`
