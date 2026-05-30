package proto

import (
	"encoding/json"
	"reflect"
	"testing"
)

// TestPhase3RoundTrip exercises Encode → Decode → As for each new Phase 3 payload
// so the wire contract can't silently drift on a struct-tag change.
func TestPhase3RoundTrip(t *testing.T) {
	t.Run("SessionCreate", func(t *testing.T) {
		in := SessionCreatePayload{
			ReqID: "r1", SessionID: "s1", Kind: SessionClaude, Cwd: "/p",
			ResumeID: "old", SkipPerms: true,
		}
		var out SessionCreatePayload
		roundTrip(t, TypeSessionCreate, in, &out)
		if !reflect.DeepEqual(in, out) {
			t.Fatalf("got %+v want %+v", out, in)
		}
	})

	t.Run("SessionCreated", func(t *testing.T) {
		in := SessionCreatedPayload{ReqID: "r1", SessionID: "s1", PID: 42, ClaudeSessionID: "s1"}
		var out SessionCreatedPayload
		roundTrip(t, TypeSessionCreated, in, &out)
		if in != out {
			t.Fatalf("got %+v want %+v", out, in)
		}
	})

	t.Run("SessionListResult", func(t *testing.T) {
		in := SessionListResultPayload{Sessions: []SessionDescriptor{
			{SessionID: "s1", Kind: SessionTerminal, Cwd: "/p", PID: 7, StartedAt: "2026-05-29T00:00:00Z"},
			{SessionID: "s2", Kind: SessionClaude, Cwd: "/q", ClaudeSessionID: "s2", StartedAt: "2026-05-29T00:00:01Z"},
		}}
		var out SessionListResultPayload
		roundTrip(t, TypeSessionListResult, in, &out)
		if !reflect.DeepEqual(in, out) {
			t.Fatalf("got %+v want %+v", out, in)
		}
	})

	t.Run("SessionReplayClaude", func(t *testing.T) {
		in := SessionReplayPayload{
			SessionID: "s1", Kind: SessionClaude,
			Events:    []json.RawMessage{json.RawMessage(`{"type":"assistant"}`)},
			Truncated: true,
		}
		var out SessionReplayPayload
		roundTrip(t, TypeSessionReplay, in, &out)
		if out.SessionID != in.SessionID || out.Kind != in.Kind || !out.Truncated {
			t.Fatalf("scalar mismatch: %+v", out)
		}
		if len(out.Events) != 1 || string(out.Events[0]) != `{"type":"assistant"}` {
			t.Fatalf("events mismatch: %v", out.Events)
		}
	})

	t.Run("ClaudeEvent", func(t *testing.T) {
		in := ClaudeEventPayload{SessionID: "s1", Subtype: "tool_use", Raw: json.RawMessage(`{"type":"tool_use","name":"Bash"}`)}
		var out ClaudeEventPayload
		roundTrip(t, TypeClaudeEvent, in, &out)
		if out.SessionID != in.SessionID || out.Subtype != in.Subtype || string(out.Raw) != string(in.Raw) {
			t.Fatalf("got %+v want %+v", out, in)
		}
	})

	t.Run("ClaudePermission", func(t *testing.T) {
		in := ClaudePermissionPayload{SessionID: "s1", ToolUseID: "tu_1", Allow: true}
		var out ClaudePermissionPayload
		roundTrip(t, TypeClaudePermission, in, &out)
		if in != out {
			t.Fatalf("got %+v want %+v", out, in)
		}
	})

	t.Run("CapabilitiesInRegister", func(t *testing.T) {
		in := RegisterPayload{
			Token: "t", Hostname: "h", OS: "darwin", Arch: "arm64", Protocol: ProtocolVersion,
			Capabilities: Capabilities{ClaudeInstalled: true, ClaudeVersion: "2.1.137", NodeInstalled: true, NodeVersion: "v22"},
		}
		var out RegisterPayload
		roundTrip(t, TypeRegister, in, &out)
		if out.Capabilities != in.Capabilities {
			t.Fatalf("caps mismatch: %+v", out.Capabilities)
		}
	})
}

// roundTrip encodes payload, decodes the envelope, and lifts it back into out.
func roundTrip[T any](t *testing.T, mt MessageType, payload any, out *T) {
	t.Helper()
	b, err := Encode(mt, payload)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	env, err := Decode(b)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if env.Type != mt {
		t.Fatalf("type: got %q want %q", env.Type, mt)
	}
	if err := As(env, out); err != nil {
		t.Fatalf("as: %v", err)
	}
}
