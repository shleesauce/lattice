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
)

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
