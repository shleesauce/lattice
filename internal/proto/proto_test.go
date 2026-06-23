package proto

import (
	"reflect"
	"testing"
)

// TestPhase3RoundTrip exercises Encode → Decode → As for each new Phase 3 payload
// so the wire contract can't silently drift on a struct-tag change.
func TestPhase3RoundTrip(t *testing.T) {
	t.Run("SessionCreate", func(t *testing.T) {
		in := SessionCreatePayload{
			ReqID: "r1", SessionID: "s1", Kind: SessionClaude, Cwd: "/p",
			ResumeID: "old",
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
		// D35: claude replays as base64 PTY bytes, exactly like a terminal.
		in := SessionReplayPayload{
			SessionID: "s1", Kind: SessionClaude,
			Data:      "aGVsbG8=",
			Truncated: true,
		}
		var out SessionReplayPayload
		roundTrip(t, TypeSessionReplay, in, &out)
		if out.SessionID != in.SessionID || out.Kind != in.Kind || !out.Truncated {
			t.Fatalf("scalar mismatch: %+v", out)
		}
		if out.Data != in.Data {
			t.Fatalf("data mismatch: %q", out.Data)
		}
	})

	t.Run("Update", func(t *testing.T) {
		// v0.1.5 (H): the hub→agent fleet-update request and its result.
		in := UpdatePayload{ReqID: "u1", Base: "http://127.0.0.1:9/dl", Version: "v0.1.5"}
		var out UpdatePayload
		roundTrip(t, TypeUpdate, in, &out)
		if in != out {
			t.Fatalf("update got %+v want %+v", out, in)
		}
	})

	t.Run("UpdateResult", func(t *testing.T) {
		in := UpdateResultPayload{ReqID: "u1", OK: true, Restarted: "sh.lattice.agent"}
		var out UpdateResultPayload
		roundTrip(t, TypeUpdateResult, in, &out)
		if in != out {
			t.Fatalf("update_result got %+v want %+v", out, in)
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

	t.Run("IdentityInRegister", func(t *testing.T) {
		// v0.2.0: the persistent agent id + per-process instance nonce ride the
		// register frame additively.
		in := RegisterPayload{
			Token: "t", Hostname: "h", OS: "darwin", Arch: "arm64", Protocol: ProtocolVersion,
			AgentUUID: "550e8400-e29b-41d4-a716-446655440000", InstanceID: "deadbeefcafef00d",
		}
		var out RegisterPayload
		roundTrip(t, TypeRegister, in, &out)
		if out.AgentUUID != in.AgentUUID || out.InstanceID != in.InstanceID {
			t.Fatalf("identity mismatch: got uuid=%q instance=%q", out.AgentUUID, out.InstanceID)
		}
	})

	t.Run("IdentityOmittedWhenEmpty", func(t *testing.T) {
		// A pre-v0.2.0-shaped register (no identity fields) must omit them on the
		// wire so the hub can tell "absent" from "present" — the duel detector and
		// the legacy-id fallback both hinge on emptiness.
		b, err := Encode(TypeRegister, RegisterPayload{Token: "t", Hostname: "h", OS: "linux"})
		if err != nil {
			t.Fatalf("encode: %v", err)
		}
		if s := string(b); contains(s, "agentUuid") || contains(s, "instanceId") {
			t.Fatalf("empty identity fields must be omitted, got: %s", s)
		}
	})
}

// contains is a tiny substring helper kept local to the test (no strings import
// churn in the production file's test).
func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
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
