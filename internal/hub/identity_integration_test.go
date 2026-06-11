package hub

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"github.com/shleesauce/lattice/internal/proto"
)

// newIdentityTestHub builds a minimal hub wired for the agent-register path: a
// real store + registry + duel guard + a known master token.
func newIdentityTestHub(t *testing.T) *Hub {
	t.Helper()
	store, err := OpenStore(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { store.Close() })
	return &Hub{store: store, registry: NewRegistry(), token: "testtoken", duel: newDuelGuard()}
}

// dialRegister opens a /ws/agent connection to srv, sends one register frame, and
// returns the connection plus the registered ack. The caller closes the conn.
func dialRegister(t *testing.T, srv *httptest.Server, reg proto.RegisterPayload) (*websocket.Conn, proto.RegisteredPayload) {
	t.Helper()
	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http")
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	frame, err := proto.Encode(proto.TypeRegister, reg)
	if err != nil {
		t.Fatalf("encode register: %v", err)
	}
	if err := conn.WriteMessage(websocket.TextMessage, frame); err != nil {
		t.Fatalf("write register: %v", err)
	}
	conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	_, raw, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("read ack: %v", err)
	}
	conn.SetReadDeadline(time.Time{})
	env, err := proto.Decode(raw)
	if err != nil {
		t.Fatalf("decode ack: %v", err)
	}
	var ack proto.RegisteredPayload
	if err := proto.As(env, &ack); err != nil {
		t.Fatalf("as ack: %v", err)
	}
	return conn, ack
}

// waitForAgent polls the registry until an id is present (the register handler
// runs putAgent after sending the ack, so a test that reads the ack must wait for
// membership before driving a second connection).
func waitForAgent(t *testing.T, h *Hub, id string) {
	t.Helper()
	for i := 0; i < 200; i++ {
		if _, ok := h.registry.getAgent(id); ok {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("agent %q never appeared in registry", id)
}

// TestRegisterLegacyContinuity proves an already-enrolled box that registers with
// NO persistent id keeps its legacy hostname+os id — the live-fleet migration
// guarantee (sessions keyed on that id don't orphan).
func TestRegisterLegacyContinuity(t *testing.T) {
	h := newIdentityTestHub(t)
	now := time.Now()
	if err := h.store.UpsertAgent(AgentRecord{
		ID: "studio-darwin", Name: "studio", Hostname: "studio", OS: "darwin", Arch: "arm64",
		FirstSeen: now, LastSeen: now,
	}); err != nil {
		t.Fatalf("seed agent: %v", err)
	}
	srv := httptest.NewServer(http.HandlerFunc(h.handleAgentWS))
	defer srv.Close()

	conn, ack := dialRegister(t, srv, proto.RegisterPayload{
		Token: "testtoken", Hostname: "studio", OS: "darwin", InstanceID: "inst-1",
	})
	defer conn.Close()
	if !ack.OK || ack.AgentID != "studio-darwin" {
		t.Fatalf("expected continuity id studio-darwin, got ok=%v id=%q", ack.OK, ack.AgentID)
	}
}

// TestRegisterMintsUUIDForNewBox proves a brand-new box (no record) gets a fresh
// id distinct from the legacy hostname+os form.
func TestRegisterMintsUUIDForNewBox(t *testing.T) {
	h := newIdentityTestHub(t)
	srv := httptest.NewServer(http.HandlerFunc(h.handleAgentWS))
	defer srv.Close()

	conn, ack := dialRegister(t, srv, proto.RegisterPayload{
		Token: "testtoken", Hostname: "fresh", OS: "linux", InstanceID: "inst-1",
	})
	defer conn.Close()
	if !ack.OK {
		t.Fatalf("register failed: %s", ack.Error)
	}
	if ack.AgentID == "" || ack.AgentID == "fresh-linux" {
		t.Fatalf("expected a fresh UUID, got %q", ack.AgentID)
	}
}

// TestRegisterDuelEndToEnd is the centerpiece proof: two live processes claiming
// one id are adjudicated (newcomer wins, loser banished + refused on reconnect),
// while a benign same-instance reconnect is never flagged.
func TestRegisterDuelEndToEnd(t *testing.T) {
	h := newIdentityTestHub(t)
	srv := httptest.NewServer(http.HandlerFunc(h.handleAgentWS))
	defer srv.Close()

	const id = "550e8400-e29b-41d4-a716-446655440000"

	// P1 (instance A) registers and stays live.
	c1, ack1 := dialRegister(t, srv, proto.RegisterPayload{
		Token: "testtoken", Hostname: "boxA", OS: "darwin", AgentUUID: id, InstanceID: "A",
	})
	defer c1.Close()
	if !ack1.OK || ack1.AgentID != id {
		t.Fatalf("P1 register: ok=%v id=%q", ack1.OK, ack1.AgentID)
	}
	waitForAgent(t, h, id)

	// P2 (instance B) registers for the SAME id while P1 is live → DUEL. Newcomer
	// wins: P2 is accepted; P1's instance A is banished.
	c2, ack2 := dialRegister(t, srv, proto.RegisterPayload{
		Token: "testtoken", Hostname: "boxA", OS: "darwin", AgentUUID: id, InstanceID: "B",
	})
	defer c2.Close()
	if !ack2.OK {
		t.Fatalf("P2 (newcomer) should win the duel, got reject: %s", ack2.Error)
	}
	if !h.duel.isBanished(id, "A", time.Now()) {
		t.Fatal("loser instance A should be banished after the duel")
	}

	// P1's reconnect (instance A) is now REFUSED — this is what stops the storm.
	c3, ack3 := dialRegister(t, srv, proto.RegisterPayload{
		Token: "testtoken", Hostname: "boxA", OS: "darwin", AgentUUID: id, InstanceID: "A",
	})
	defer c3.Close()
	if ack3.OK {
		t.Fatal("banished instance A must be refused on reconnect")
	}

	// A benign reconnect of the WINNER (same instance B) is accepted, not a duel.
	waitForAgent(t, h, id)
	c4, ack4 := dialRegister(t, srv, proto.RegisterPayload{
		Token: "testtoken", Hostname: "boxA", OS: "darwin", AgentUUID: id, InstanceID: "B",
	})
	defer c4.Close()
	if !ack4.OK {
		t.Fatalf("same-instance reconnect must be accepted, got: %s", ack4.Error)
	}
	if h.duel.isBanished(id, "B", time.Now()) {
		t.Fatal("winner instance B must never be banished")
	}
}
