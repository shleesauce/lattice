package hub

import (
	"testing"
	"time"
)

func TestApprovalMintConsumeSingleUse(t *testing.T) {
	a := newApprovalStore()
	now := time.Now()
	nonce := a.mint("sess-1", "studio-darwin", now)
	if nonce == "" {
		t.Fatal("mint returned empty nonce")
	}

	got, ok := a.consume(nonce, now)
	if !ok {
		t.Fatal("consume rejected a fresh nonce")
	}
	if got.sessionID != "sess-1" || got.agentID != "studio-darwin" {
		t.Fatalf("consume returned wrong payload: %+v", got)
	}

	// Single-shot: a second consume of the same nonce must fail.
	if _, ok := a.consume(nonce, now); ok {
		t.Fatal("consume accepted an already-used nonce")
	}
}

func TestApprovalUnknownNonceRejected(t *testing.T) {
	a := newApprovalStore()
	if _, ok := a.consume("nope", time.Now()); ok {
		t.Fatal("consume accepted an unknown nonce")
	}
}

func TestApprovalExpires(t *testing.T) {
	a := newApprovalStore()
	now := time.Now()
	nonce := a.mint("sess-1", "studio-darwin", now)
	// Past the TTL the link is dead even though it was never used.
	if _, ok := a.consume(nonce, now.Add(approvalTTL+time.Minute)); ok {
		t.Fatal("consume accepted an expired nonce")
	}
}

func TestApprovalDropForSession(t *testing.T) {
	a := newApprovalStore()
	now := time.Now()
	n1 := a.mint("sess-1", "studio-darwin", now)
	n2 := a.mint("sess-2", "mbp-darwin", now)

	a.dropForSession("sess-1")

	if _, ok := a.consume(n1, now); ok {
		t.Fatal("dropForSession left sess-1's nonce live")
	}
	if _, ok := a.consume(n2, now); !ok {
		t.Fatal("dropForSession wrongly dropped an unrelated session's nonce")
	}
}

func TestApprovalExpectedExit(t *testing.T) {
	a := newApprovalStore()
	now := time.Now()

	// An unmarked exit is "unexpected" (worth a finished ping).
	if a.takeExpected("sess-x") {
		t.Fatal("takeExpected reported an unmarked session as expected")
	}

	a.expectExit("sess-x", now)
	if !a.takeExpected("sess-x") {
		t.Fatal("takeExpected failed to report a hub-initiated close")
	}
	// takeExpected clears the marker — a second call is false.
	if a.takeExpected("sess-x") {
		t.Fatal("takeExpected did not clear the expected marker")
	}
}

func TestApprovalSweep(t *testing.T) {
	a := newApprovalStore()
	now := time.Now()
	nonce := a.mint("sess-1", "studio-darwin", now)
	a.expectExit("sess-1", now)

	a.sweep(now.Add(approvalTTL + time.Minute))

	if _, ok := a.consume(nonce, now); ok {
		t.Fatal("sweep left an expired nonce behind")
	}
	if a.takeExpected("sess-1") {
		t.Fatal("sweep left a stale expected-exit marker behind")
	}
}

func TestApprovalNonceUnique(t *testing.T) {
	a := newApprovalStore()
	now := time.Now()
	seen := make(map[string]bool)
	for i := 0; i < 256; i++ {
		n := a.mint("s", "agent", now)
		if seen[n] {
			t.Fatalf("duplicate nonce minted: %q", n)
		}
		seen[n] = true
	}
}

func TestPrettyAgentName(t *testing.T) {
	cases := map[string]string{
		"studio-darwin":     "studio",
		"desktop-x-windows": "desktop-x",
		"box-linux":         "box",
		"no-suffix":         "no-suffix",
	}
	for in, want := range cases {
		if got := prettyAgentName(in); got != want {
			t.Errorf("prettyAgentName(%q) = %q, want %q", in, got, want)
		}
	}
}
