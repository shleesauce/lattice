// Package proto defines the wire contract between the Lattice hub and agents.
//
// Both roles live in the same binary and import this package, so the message
// types here ARE the contract — there is no second source of truth to drift
// against. Transport is a single persistent WebSocket that the agent dials OUT
// to the hub (the hub never connects to an agent). Every frame is a JSON
// Envelope sent as a WebSocket text message.
package proto

import (
	"encoding/json"
	"fmt"
)

// ProtocolVersion is bumped on any breaking change to the message set.
const ProtocolVersion = 1

// MessageType enumerates the envelope kinds. Agent→Hub and Hub→Agent share one
// space so a single switch can route either direction.
type MessageType string

const (
	// Agent → Hub
	TypeRegister      MessageType = "register"       // first frame after dial
	TypeHeartbeat     MessageType = "heartbeat"      // periodic liveness + metrics
	TypeCommandOutput MessageType = "command_output" // stdout/stderr chunk
	TypeCommandExit   MessageType = "command_exit"   // command finished

	// Hub → Agent
	TypeRegistered MessageType = "registered"  // ack of register
	TypeRunCommand MessageType = "run_command" // one-shot command request
	TypePing       MessageType = "ping"        // hub keepalive (agent need not reply)

	// --- Phase 2: interactive terminal (PTY) ---
	// Hub → Agent
	TypeTermStart  MessageType = "term_start"  // open a PTY session
	TypeTermInput  MessageType = "term_input"  // keystrokes to the PTY (Data base64)
	TypeTermResize MessageType = "term_resize" // window resize
	TypeTermClose  MessageType = "term_close"  // close the PTY session
	// Agent → Hub
	TypeTermOutput MessageType = "term_output" // PTY output (Data base64)
	TypeTermExit   MessageType = "term_exit"   // PTY session ended

	// --- Phase 2: scoped file browser (request/response, correlated by ReqID) ---
	// Hub → Agent
	TypeFileList MessageType = "file_list" // list a directory
	TypeFileGet  MessageType = "file_get"  // fetch a file's bytes (size-capped)
	// Agent → Hub
	TypeFileListResult MessageType = "file_list_result"
	TypeFileGetResult  MessageType = "file_get_result"

	// --- Phase 2: Wake-on-LAN (request/response, correlated by ReqID) ---
	// Hub → Agent
	TypeWake MessageType = "wake" // send a magic packet on this agent's LAN
	// Agent → Hub
	TypeWakeResult MessageType = "wake_result"

	// --- v0.1.5 (Phase F): power control — sleep/shutdown a machine via its own
	// agent so the fleet can run a true unattended loop (wake → work → sleep).
	// Hub → Agent
	TypePowerControl MessageType = "power_control"
	// Agent → Hub
	TypePowerControlResult MessageType = "power_control_result"

	// --- Session transcript (F16): read a session's saved Claude transcript ---
	// Transcripts are deliberately NOT Syncthing-synced (~/.claude/.stignore
	// "**/*.jsonl" — huge, machine-local, race-prone) AND claude sessions never run
	// on the hub box (F14), so the hub can't read them off its own disk. Each agent
	// serves its OWN machine's transcript: the hub round-trips this to the session's
	// owning agent, correlated by ReqID, same as the file browser. Hub → Agent:
	TypeTranscriptGet MessageType = "transcript_get"
	// Agent → Hub
	TypeTranscriptResult MessageType = "transcript_result"

	// --- Phase 3: long-lived sessions (lifecycle; create/list correlated by ReqID) ---
	// A session is a terminal OR claude process that lives on the agent and OUTLIVES
	// the browser WebSocket. The hub persists a row; the agent owns the process + a
	// scrollback buffer. Browsers attach/detach without killing it.
	// Hub → Agent
	TypeSessionCreate MessageType = "session_create" // start a long-lived terminal|claude session
	TypeSessionAttach MessageType = "session_attach" // a browser attached → reply with replay
	TypeSessionDetach MessageType = "session_detach" // the browser detached → keep the process alive
	TypeSessionClose  MessageType = "session_close"  // terminate the session process for good
	TypeSessionList   MessageType = "session_list"   // ask the agent to enumerate its live sessions
	// Agent → Hub
	TypeSessionCreated    MessageType = "session_created"     // ack of session_create (pid, claudeId)
	TypeSessionReplay     MessageType = "session_replay"      // scrollback / event tail on attach
	TypeSessionExit       MessageType = "session_exit"        // the session process ended
	TypeSessionListResult MessageType = "session_list_result" // live sessions (also sent post-register)
	TypeSessionIdle       MessageType = "session_idle"        // a claude session went quiet (Idle=true) / resumed (Idle=false)

	// --- v0.1.5 (H): one-click fleet auto-update (request/response by ReqID) ---
	// The hub self-updates first, then cascades this to every online agent IN
	// LOCKSTEP so the whole fleet lands on ONE build and the wire contract can't
	// skew mid-flight (D34). Hub → Agent:
	TypeUpdate MessageType = "update" // pull+verify+swap the release binary, then restart this agent's service
	// Agent → Hub
	TypeUpdateResult MessageType = "update_result"

	// --- IDE milestone (M2): embedded editor (code-server) over a yamux tunnel ---
	// An editor session is a code-server process bound to 127.0.0.1 on the agent.
	// Its HTTP/WS traffic never crosses the wire directly: the hub reverse-proxies
	// /editor/{sessionId}/* over a SECOND dial-out WebSocket multiplexed with yamux
	// (D27) — preserving D2 (zero inbound on leaves). Lifecycle reuses the Phase-3
	// session machinery above (session_create/attach/close with Kind=editor); the
	// only editor-specific wire piece is the tunnel transport, which carries raw
	// yamux streams, NOT proto Envelopes. So there are no new MessageTypes here —
	// just the SessionEditor kind and the Capabilities fields added below.
)

// FileGetMaxBytes caps a single file_get response (base64 over the JSON WS).
const FileGetMaxBytes = 10 << 20 // 10 MiB

// --- Phase 2 payloads: terminal ---

// TermStartPayload opens a PTY. Shell empty ⇒ agent picks the OS default.
type TermStartPayload struct {
	TermID string `json:"termId"`
	Shell  string `json:"shell,omitempty"`
	Cols   uint16 `json:"cols"`
	Rows   uint16 `json:"rows"`
}

// TermDataPayload carries terminal bytes in both directions. Data is base64 so
// raw control bytes / partial UTF-8 survive the JSON transport intact.
type TermDataPayload struct {
	TermID string `json:"termId"`
	Data   string `json:"data"` // base64-encoded bytes
}

// TermResizePayload resizes a live PTY.
type TermResizePayload struct {
	TermID string `json:"termId"`
	Cols   uint16 `json:"cols"`
	Rows   uint16 `json:"rows"`
}

// TermControlPayload references a PTY session (close / exit).
type TermControlPayload struct {
	TermID   string `json:"termId"`
	ExitCode int    `json:"exitCode,omitempty"`
	Error    string `json:"error,omitempty"`
}

// --- Phase 2 payloads: file browser ---

// FileReqPayload requests a directory listing or a file fetch.
type FileReqPayload struct {
	ReqID string `json:"reqId"`
	Path  string `json:"path"` // empty ⇒ agent's home directory
}

// FileEntry is one item in a directory listing.
type FileEntry struct {
	Name    string `json:"name"`
	Path    string `json:"path"`
	IsDir   bool   `json:"isDir"`
	Size    int64  `json:"size"`
	ModTime string `json:"modTime"` // RFC3339
}

// FileListResultPayload answers a file_list.
type FileListResultPayload struct {
	ReqID   string      `json:"reqId"`
	Path    string      `json:"path"`    // resolved absolute path listed
	Parent  string      `json:"parent"`  // parent dir for up-navigation
	Entries []FileEntry `json:"entries"` // dirs first, then files (agent sorts)
	Error   string      `json:"error,omitempty"`
}

// FileGetResultPayload answers a file_get. Content is base64; truncated if the
// file exceeds FileGetMaxBytes (Truncated=true).
type FileGetResultPayload struct {
	ReqID     string `json:"reqId"`
	Path      string `json:"path"`
	Name      string `json:"name"`
	Size      int64  `json:"size"`
	Content   string `json:"content"` // base64
	Truncated bool   `json:"truncated,omitempty"`
	Error     string `json:"error,omitempty"`
}

// --- Session transcript payloads (F16) ---

// TranscriptReqPayload asks an agent for a session's saved Claude transcript. The
// agent globs its OWN ~/.claude/projects/*/<sessionId>.jsonl (sessionId == claude's
// --session-id == the filename stem), parses it, and answers with the normalized
// turns. ClaudeSessionID is the explicit filename stem when it differs from the
// Lattice session id (it doesn't today, but resume could change that); empty ⇒ use
// SessionID.
type TranscriptReqPayload struct {
	ReqID           string `json:"reqId"`
	SessionID       string `json:"sessionId"`
	ClaudeSessionID string `json:"claudeSessionId,omitempty"`
}

// TranscriptResultPayload answers a transcript_get. Blocks and Meta carry the
// agent-parsed transcript ([]transcript.Block / transcript.Meta) as raw JSON so
// this contract stays free of the transcript package; the hub forwards them to the
// browser unchanged. Found=false (not Error) means simply "no .jsonl on this box".
type TranscriptResultPayload struct {
	ReqID  string          `json:"reqId"`
	Found  bool            `json:"found"`
	Path   string          `json:"path,omitempty"`
	Meta   json.RawMessage `json:"meta,omitempty"`
	Blocks json.RawMessage `json:"blocks,omitempty"`
	Error  string          `json:"error,omitempty"`
}

// --- Phase 2 payloads: Wake-on-LAN ---

// WakePayload asks the agent to broadcast a WoL magic packet for the target MAC.
type WakePayload struct {
	ReqID string `json:"reqId"`
	MAC   string `json:"mac"` // aa:bb:cc:dd:ee:ff or aa-bb-... etc.
}

// WakeResultPayload answers a wake.
type WakeResultPayload struct {
	ReqID string `json:"reqId"`
	OK    bool   `json:"ok"`
	Error string `json:"error,omitempty"`
}

// --- v0.1.5 (Phase F) payloads: power control ---

// PowerAction is what a PowerControlPayload asks the agent to do to its own host.
type PowerAction string

const (
	PowerSleep    PowerAction = "sleep"    // suspend to RAM (wakeable via WoL after)
	PowerShutdown PowerAction = "shutdown" // full power off
)

// PowerControlPayload asks an agent to sleep or shut down its OWN machine. There
// is no "wake" action here — waking a sleeping box is WoL (TypeWake from a LAN
// peer), since a slept agent isn't connected to receive a frame.
type PowerControlPayload struct {
	ReqID  string      `json:"reqId"`
	Action PowerAction `json:"action"`
}

// PowerControlResultPayload answers a power_control. OK=true means the command was
// accepted/issued (the agent may go offline immediately after, so this is the last
// frame it sends).
type PowerControlResultPayload struct {
	ReqID  string `json:"reqId"`
	Action string `json:"action"`
	OK     bool   `json:"ok"`
	Error  string `json:"error,omitempty"`
}

// --- v0.1.5 (H) payloads: one-click fleet auto-update ---

// UpdatePayload asks an agent to pull+verify+swap the release binary and restart
// its own service. Base is the hub's RESOLVED download base, threaded down so
// every agent fetches the identical build the hub just installed (lockstep, D34) —
// not whatever each agent's own $LATTICE_DOWNLOAD_BASE might resolve to. Version
// is the target version string (e.g. "v0.1.5"), informational for logging/result.
type UpdatePayload struct {
	ReqID   string `json:"reqId"`
	Base    string `json:"base,omitempty"`
	Version string `json:"version,omitempty"`
}

// UpdateResultPayload answers an update. OK=true means the binary was verified
// and swapped on this agent; Restarted is the service label the agent kicked (or
// "" if it couldn't find one — the new binary still applies on the next start).
// A non-empty Error means the fail-closed verify or swap aborted and the agent is
// STILL on its old binary (so the hub can surface a partial-fleet result).
type UpdateResultPayload struct {
	ReqID     string `json:"reqId"`
	OK        bool   `json:"ok"`
	Restarted string `json:"restarted,omitempty"`
	Error     string `json:"error,omitempty"`
}

// --- Phase 3 payloads: long-lived sessions ---

// SessionKind discriminates the two long-lived session types.
type SessionKind string

const (
	SessionTerminal SessionKind = "terminal"
	// SessionClaude is an INTERACTIVE `claude` process in a PTY (D35, supersedes
	// D17/D21). It speaks the SAME frames as a terminal — output/replay (base64 PTY
	// bytes) out, input/resize in. It is NOT headless stream-json: starting June 15
	// 2026, headless `claude -p`/Agent-SDK usage on subscription plans bills against
	// a separate capped credit pool, while interactive claude in a real TTY stays on
	// normal subscription limits. The agent launches it via the ptySession path.
	SessionClaude SessionKind = "claude"
	// SessionEditor is an embedded code-server instance (IDE milestone, D28). It
	// reuses the long-lived session lifecycle; the agent spawns code-server bound
	// to loopback and the hub proxies it over the yamux tunnel.
	SessionEditor SessionKind = "editor"
)

// Session status values persisted by the hub. Kept here so hub + (future) agent
// agree on the vocabulary.
const (
	SessionStarting = "starting" // create dispatched, ack not yet received
	SessionLive     = "live"     // process running, agent online
	SessionDetached = "detached" // running, no browser attached (informational)
	SessionExited   = "exited"   // process ended (natural or closed)
	SessionOrphaned = "orphaned" // agent went offline; resumable elsewhere
)

// TermRingBytes bounds the per-terminal-session scrollback ring.
const TermRingBytes = 256 << 10 // 256 KiB

// SessionCreatePayload opens a long-lived session on the agent. The hub assigns
// SessionID (a UUID) BEFORE dispatch so the DB row, re-discovery, and — for claude
// sessions — the `claude --session-id` all agree on one identifier. For kind=claude,
// ResumeID (when set) resumes a prior logical Claude conversation from the synced
// transcript (D20). Cwd is the project path the session runs in.
type SessionCreatePayload struct {
	ReqID     string      `json:"reqId"`
	SessionID string      `json:"sessionId"`
	Kind      SessionKind `json:"kind"`
	Cwd       string      `json:"cwd"`                // project path
	Shell     string      `json:"shell,omitempty"`    // terminal only; empty ⇒ OS default
	Cols      uint16      `json:"cols,omitempty"`     // terminal only
	Rows      uint16      `json:"rows,omitempty"`     // terminal only
	ResumeID  string      `json:"resumeId,omitempty"` // claude: prior claudeSessionId to --resume
	// PermissionMode is the claude --permission-mode for this session (default,
	// acceptEdits, plan, auto, bypassPermissions, dontAsk). Empty/invalid ⇒
	// bypassPermissions (the Lattice default — sessions are often unattended).
	PermissionMode string `json:"permissionMode,omitempty"`
	// Model is the claude --model for this session — a full model id (e.g.
	// claude-opus-4-8, or the 1M-context form claude-opus-4-8[1m]). Empty ⇒ the
	// agent passes no --model, leaving claude on its own configured default. The
	// agent validates against an allow-list so a bad value can't reach the launch.
	// Persisted on the session row so --resume relaunches with the same model (D20).
	Model string `json:"model,omitempty"`
	// FastMode requests claude's low-effort ("fast") setting — mapped to
	// `--effort low` at launch. Empty/false ⇒ no --effort flag (claude's default).
	// A launch-time preference like PermissionMode: not persisted, not carried on
	// resume.
	FastMode bool `json:"fastMode,omitempty"`
	// HubURL + HookToken wire Lattice-managed Claude Code hooks (C) back to the hub.
	// The agent adds `--settings <static lattice hooks file>` and injects these into
	// the claude child env (cmd.Env): the hook scripts curl-POST {sessionId, event,
	// token} to <HubURL>/api/hooks/state for precise turn-done / awaiting-approval /
	// session-end state. HookToken is a per-session capability (the credential the
	// ungated hooks endpoint validates). Empty HubURL ⇒ the agent skips --settings
	// and the hub falls back to the PTY-quiet idle heuristic.
	HubURL    string `json:"hubUrl,omitempty"`
	HookToken string `json:"hookToken,omitempty"`
	// SeedInput, if set, is typed into the session ONCE the interactive TUI has
	// settled (claude: the onboarding brief). The agent waits for output to go quiet
	// before injecting, so the keystrokes aren't dropped during boot/first render.
	SeedInput string `json:"seedInput,omitempty"`
}

// SessionCreatedPayload acks a session_create.
type SessionCreatedPayload struct {
	ReqID           string `json:"reqId"`
	SessionID       string `json:"sessionId"`
	PID             int    `json:"pid,omitempty"`
	ClaudeSessionID string `json:"claudeSessionId,omitempty"` // = SessionID for claude (hub-assigned)
	Error           string `json:"error,omitempty"`
}

// SessionDescriptor describes one live session the agent owns. Reported in
// session_list_result (and right after register, for re-discovery — F).
type SessionDescriptor struct {
	SessionID       string      `json:"sessionId"`
	Kind            SessionKind `json:"kind"`
	Cwd             string      `json:"cwd"`
	ClaudeSessionID string      `json:"claudeSessionId,omitempty"`
	PID             int         `json:"pid,omitempty"`
	StartedAt       string      `json:"startedAt"` // RFC3339
}

// SessionListResultPayload answers session_list, and is also volunteered by the
// agent immediately after registered (live-session re-discovery).
type SessionListResultPayload struct {
	ReqID    string              `json:"reqId,omitempty"`
	Sessions []SessionDescriptor `json:"sessions"`
}

// SessionAttachPayload tells the agent a browser attached; the agent replies with
// a session_replay carrying the current scrollback/event tail. Cols/Rows re-fit a
// terminal on attach.
type SessionAttachPayload struct {
	SessionID string `json:"sessionId"`
	Cols      uint16 `json:"cols,omitempty"`
	Rows      uint16 `json:"rows,omitempty"`
}

// SessionReplayPayload dumps recent output so a re-attaching browser sees context.
// Data is base64 PTY scrollback for BOTH terminal and claude sessions (D35): claude
// is now an interactive PTY, so it replays raw bytes exactly like a shell.
type SessionReplayPayload struct {
	SessionID string      `json:"sessionId"`
	Kind      SessionKind `json:"kind"`
	Data      string      `json:"data,omitempty"` // PTY scrollback, base64
	Truncated bool        `json:"truncated,omitempty"`
}

// SessionControlPayload references a session for detach / close / exit.
type SessionControlPayload struct {
	SessionID string `json:"sessionId"`
	ExitCode  int    `json:"exitCode,omitempty"`
	Error     string `json:"error,omitempty"`
}

// SessionIdlePayload reports a claude session crossing the quiet threshold
// (Idle=true: no PTY output for QuietMs, so claude is waiting on input — a
// permission gate or just the end of its turn) or resuming output after being
// idle (Idle=false). The hub uses the true edge to fire the fire-and-forget
// "needs you" notification for opt-in sessions; both edges feed the audit_log.
type SessionIdlePayload struct {
	SessionID string `json:"sessionId"`
	Idle      bool   `json:"idle"`
	QuietMs   int64  `json:"quietMs,omitempty"`
}

// --- Phase 3 payloads: capabilities ---

// Capabilities is what an agent can run — the placement hard filter (D19) reads it.
// Embedded in RegisterPayload and refreshed via HeartbeatPayload.
type Capabilities struct {
	ClaudeInstalled bool   `json:"claudeInstalled"`
	ClaudeVersion   string `json:"claudeVersion,omitempty"`
	// ClaudeAuthable reports whether this agent can actually COMPLETE Claude's
	// OAuth sign-in — not just that the binary is present (D22, F14). On macOS the
	// claude CLI reads its OAuth token from the login Keychain, reachable only from
	// a process launched by launchd as a GUI LaunchAgent; an agent run under
	// pm2/nohup can't unlock it and claude hangs on a blank auth prompt. Placement
	// (and the machine switcher) exclude non-authable agents from claude sessions so
	// switching/creating a Claude session never lands on a box that yields a dead
	// blank tab. ClaudeInstalled && !ClaudeAuthable is exactly the pm2-on-macOS
	// hub-host case.
	ClaudeAuthable bool   `json:"claudeAuthable"`
	NodeInstalled  bool   `json:"nodeInstalled"`
	NodeVersion    string `json:"nodeVersion,omitempty"`
	// IDE milestone (D28/D30): can this agent host an embedded editor? code-server
	// must be installed (per-node install, P1 decision). On Windows it runs inside
	// WSL2, so WSLAvailable gates the editor there.
	CodeServerInstalled bool   `json:"codeServerInstalled"`
	CodeServerVersion   string `json:"codeServerVersion,omitempty"`
	WSLAvailable        bool   `json:"wslAvailable,omitempty"`
	// SyncthingInstalled/Running feed the integrations panel (Phase 4). Installed
	// is a binary-resolve; Running is a cheap TCP dial to the local Syncthing GUI/
	// API port (127.0.0.1:8384). Cached 5min like the rest of the capability set.
	SyncthingInstalled bool `json:"syncthingInstalled"`
	SyncthingRunning   bool `json:"syncthingRunning"`
}

// Envelope wraps every message. Payload is the type-specific body.
type Envelope struct {
	Type    MessageType     `json:"type"`
	Payload json.RawMessage `json:"payload,omitempty"`
}

// --- Agent → Hub payloads ---

// RegisterPayload is the agent's identity + the enrollment token. The hub binds
// the resulting agent record to the (token-authorized) connection.
type RegisterPayload struct {
	Token        string `json:"token"`
	Hostname     string `json:"hostname"`
	OS           string `json:"os"`   // runtime.GOOS: darwin|windows|linux
	Arch         string `json:"arch"` // runtime.GOARCH: arm64|amd64
	AgentVersion string `json:"agentVersion"`
	Protocol     int    `json:"protocol"`
	// Phase 3 (additive): what this agent can run, for placement (D19).
	Capabilities Capabilities `json:"capabilities,omitempty"`

	// --- v0.2.0 identity (additive): persistent agent id + per-process instance ---
	//
	// AgentUUID is this agent's PERSISTENT, machine-stable identity. It is minted
	// once at first enrollment and stored at ~/.lattice/agent-id, so it survives a
	// hostname change and — crucially — makes two same-hostname machines DISTINCT.
	// The hub keys its registry on this id (hostname+os become display-only). Empty
	// ⇒ a pre-v0.2.0 agent OR a brand-new agent whose id the hub will ASSIGN: the
	// hub falls back to the legacy hostname+os id (reusing an existing fleet record
	// so sessions don't orphan) or mints a fresh UUID, and returns the resolved id
	// in RegisteredPayload.AgentID for the agent to persist. Once persisted, the
	// agent always sends it.
	AgentUUID string `json:"agentUuid,omitempty"`
	// InstanceID is a fresh random nonce minted every PROCESS START (never
	// persisted). Two LIVE connections claiming one AgentUUID with DIFFERENT
	// InstanceIDs are two rival processes — the reconnect-storm class — which the
	// hub now DETECTS and resolves (keep newest, banish + alarm on the loser)
	// instead of letting them duel silently. A normal network reconnect re-dials
	// with the SAME InstanceID and is never flagged. Empty ⇒ a pre-v0.2.0 agent;
	// the duel detector requires both sides non-empty, so a mixed-version fleet
	// degrades safely to the legacy behavior.
	InstanceID string `json:"instanceId,omitempty"`
}

// HeartbeatPayload carries the live metrics rendered on the dashboard. Sent on
// register and then every HeartbeatInterval.
type HeartbeatPayload struct {
	UptimeSec   uint64  `json:"uptimeSec"`
	DiskTotal   uint64  `json:"diskTotal"`   // bytes, root/system volume
	DiskFree    uint64  `json:"diskFree"`    // bytes
	DiskUsedPct float64 `json:"diskUsedPct"` // 0..100
	MemTotal    uint64  `json:"memTotal"`    // bytes
	MemUsedPct  float64 `json:"memUsedPct"`  // 0..100
	LoadAvg1    float64 `json:"loadAvg1"`    // 0 on platforms without loadavg
	CPUCount    int     `json:"cpuCount"`
	// MACs are the agent's physical-interface hardware addresses. The hub keeps
	// the last-known set so an OFFLINE machine can still be woken (WoL) by a
	// peer on its LAN — no manual MAC entry, which keeps Wake turnkey.
	MACs []string `json:"macs,omitempty"`
	// LANIPs are the agent's private-range IPv4 addresses in CIDR form
	// (e.g. "192.168.1.46/24"), one per up, non-loopback interface. The hub uses
	// these to RELAY a WoL magic packet: a sleeping box's last-known subnet is
	// matched against the subnets of LIVE agents, so the packet is broadcast from
	// a peer that actually shares the target's broadcast domain — and "no relay
	// reachable on that subnet" is surfaced instead of a packet that silently
	// goes nowhere. Persisted with the rest of the metrics blob so an OFFLINE
	// machine's last-known subnet is still known. (v0.1.5 Phase F.)
	LANIPs []string `json:"lanIPs,omitempty"`
	// Phase 3 (additive): refresh capabilities without a reconnect so placement
	// always scores fresh can-run state (D19).
	Capabilities Capabilities `json:"capabilities,omitempty"`
}

// CommandOutputPayload is one streamed chunk of a running command's output.
type CommandOutputPayload struct {
	CmdID  string `json:"cmdId"`
	Stream string `json:"stream"` // "stdout" | "stderr"
	Data   string `json:"data"`
}

// CommandExitPayload signals a command finished (or failed to start).
type CommandExitPayload struct {
	CmdID    string `json:"cmdId"`
	ExitCode int    `json:"exitCode"`
	Error    string `json:"error,omitempty"` // non-empty if the agent couldn't run it
}

// --- Hub → Agent payloads ---

// RegisteredPayload acks a register. OK=false + Error means the token/identity
// was rejected and the agent should stop retrying with the same token.
type RegisteredPayload struct {
	AgentID string `json:"agentId"`
	OK      bool   `json:"ok"`
	Error   string `json:"error,omitempty"`
}

// RunCommandPayload asks the agent to run a one-shot command via the platform
// shell and stream output back tagged with CmdID.
type RunCommandPayload struct {
	CmdID   string `json:"cmdId"`
	Command string `json:"command"`
}

// Encode builds an envelope of the given type around a payload value and
// returns the JSON bytes to write as a WebSocket text message.
func Encode(t MessageType, payload any) ([]byte, error) {
	var raw json.RawMessage
	if payload != nil {
		b, err := json.Marshal(payload)
		if err != nil {
			return nil, fmt.Errorf("proto: marshal %s payload: %w", t, err)
		}
		raw = b
	}
	return json.Marshal(Envelope{Type: t, Payload: raw})
}

// Decode parses raw frame bytes into an Envelope.
func Decode(b []byte) (Envelope, error) {
	var e Envelope
	if err := json.Unmarshal(b, &e); err != nil {
		return Envelope{}, fmt.Errorf("proto: decode envelope: %w", err)
	}
	return e, nil
}

// As unmarshals an envelope's payload into a typed value: proto.As(env, &hb).
func As[T any](e Envelope, out *T) error {
	if len(e.Payload) == 0 {
		return fmt.Errorf("proto: empty payload for %s", e.Type)
	}
	if err := json.Unmarshal(e.Payload, out); err != nil {
		return fmt.Errorf("proto: decode %s payload: %w", e.Type, err)
	}
	return nil
}
