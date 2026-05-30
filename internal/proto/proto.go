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

	// --- Phase 3: Claude session channel (streaming, keyed by SessionID) ---
	// Hub → Agent
	TypeClaudeInput      MessageType = "claude_input"      // a user turn written to the claude stdin
	TypeClaudePermission MessageType = "claude_permission" // approve/deny a tool call (approval mode)
	// Agent → Hub
	TypeClaudeEvent MessageType = "claude_event" // one stream-json event from claude, verbatim

	// --- Phase 3: agent capabilities (also folded into register + heartbeat) ---
	// Agent → Hub
	TypeCapabilities MessageType = "capabilities" // standalone capability refresh

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

// --- Phase 3 payloads: long-lived sessions ---

// SessionKind discriminates the two long-lived session types.
type SessionKind string

const (
	SessionTerminal SessionKind = "terminal"
	SessionClaude   SessionKind = "claude"
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

// ClaudeEventRingMax bounds the per-claude-session replay tail (events).
const ClaudeEventRingMax = 200

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
	SkipPerms bool        `json:"skipPerms"`          // claude: bypassPermissions (D21); false ⇒ approval mode
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
// terminal: Data is base64 PTY scrollback. claude: Events is the recent stream-json
// event tail (each element is one verbatim event object).
type SessionReplayPayload struct {
	SessionID string            `json:"sessionId"`
	Kind      SessionKind       `json:"kind"`
	Data      string            `json:"data,omitempty"`   // terminal scrollback, base64
	Events    []json.RawMessage `json:"events,omitempty"` // claude event tail
	Truncated bool              `json:"truncated,omitempty"`
}

// SessionControlPayload references a session for detach / close / exit.
type SessionControlPayload struct {
	SessionID string `json:"sessionId"`
	ExitCode  int    `json:"exitCode,omitempty"`
	Error     string `json:"error,omitempty"`
}

// --- Phase 3 payloads: Claude session channel ---

// ClaudeInputPayload is a user turn sent to a claude session's stdin. The agent
// wraps Text in the stream-json user-message envelope the CLI expects.
type ClaudeInputPayload struct {
	SessionID string `json:"sessionId"`
	Text      string `json:"text"`
}

// ClaudeEventPayload carries ONE structured stream-json event verbatim from the
// claude process stdout (assistant message, tool_use, tool_result, usage, result,
// system/init…). The hub forwards Raw to dashboards untouched and inspects Subtype
// for audit logging. Subtype is the event's top-level "type" field, lifted out for
// cheap routing without re-parsing Raw.
type ClaudeEventPayload struct {
	SessionID string          `json:"sessionId"`
	Subtype   string          `json:"subtype"`
	Raw       json.RawMessage `json:"raw"`
}

// ClaudePermissionPayload answers a tool-permission prompt when approval mode is on.
type ClaudePermissionPayload struct {
	SessionID string `json:"sessionId"`
	ToolUseID string `json:"toolUseId"`
	Allow     bool   `json:"allow"`
}

// --- Phase 3 payloads: capabilities ---

// Capabilities is what an agent can run — the placement hard filter (D19) reads it.
// Embedded in RegisterPayload and refreshed via HeartbeatPayload, and sendable
// standalone via TypeCapabilities.
type Capabilities struct {
	ClaudeInstalled bool   `json:"claudeInstalled"`
	ClaudeVersion   string `json:"claudeVersion,omitempty"`
	NodeInstalled   bool   `json:"nodeInstalled"`
	NodeVersion     string `json:"nodeVersion,omitempty"`
	// IDE milestone (D28/D30): can this agent host an embedded editor? code-server
	// must be installed (per-node install, P1 decision). On Windows it runs inside
	// WSL2, so WSLAvailable gates the editor there.
	CodeServerInstalled bool   `json:"codeServerInstalled"`
	CodeServerVersion   string `json:"codeServerVersion,omitempty"`
	WSLAvailable        bool   `json:"wslAvailable,omitempty"`
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
