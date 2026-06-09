package hub

import (
	"testing"
	"time"
)

// The hook token store validates only the exact (sessionId, token) pair it minted,
// in constant time, and rejects unknown/blank/expired tokens.
func TestHookStoreValidity(t *testing.T) {
	s := newHookStore()
	now := time.Now()
	s.register("sess1", "secret-token", now)

	if !s.valid("sess1", "secret-token") {
		t.Fatal("valid pair rejected")
	}
	if s.valid("sess1", "wrong") {
		t.Fatal("wrong token accepted")
	}
	if s.valid("sess2", "secret-token") {
		t.Fatal("token accepted for the wrong session")
	}
	if s.valid("sess1", "") {
		t.Fatal("blank token accepted")
	}

	// Drop disarms it.
	s.drop("sess1")
	if s.valid("sess1", "secret-token") {
		t.Fatal("token still valid after drop")
	}

	// Sweep expires a stale token.
	s.register("sess3", "tok3", now.Add(-2*hookTokenTTL))
	s.sweep(now)
	if s.valid("sess3", "tok3") {
		t.Fatal("expired token survived sweep")
	}
}

// mintHookToken registers a usable token and hooksEnabled gates on a hub URL.
func TestMintHookTokenAndEnabled(t *testing.T) {
	h := &Hub{hooks: newHookStore()}
	if h.hooksEnabled() {
		t.Fatal("hooksEnabled true with empty hubURL")
	}
	h.hubURL = "https://hub.example.ts.net:7400"
	if !h.hooksEnabled() {
		t.Fatal("hooksEnabled false with a hubURL set")
	}
	tok := h.mintHookToken("sX", time.Now())
	if tok == "" {
		t.Fatal("minted empty token")
	}
	if !h.hooks.valid("sX", tok) {
		t.Fatal("minted token not valid")
	}
}
