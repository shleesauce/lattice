package hub

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/shleesauce/lattice/internal/proto"
)

// persistAgent writes an offline agent row with last-known MACs + LAN CIDRs so
// fleet() surfaces it as a wakeable target.
func persistAgent(t *testing.T, h *Hub, id string, macs, lanIPs []string) {
	t.Helper()
	now := time.Now()
	if err := h.store.UpsertAgent(AgentRecord{ID: id, Name: id, Hostname: id, OS: "darwin", FirstSeen: now, LastSeen: now}); err != nil {
		t.Fatalf("upsert agent: %v", err)
	}
	if err := h.store.UpdateMetrics(id, proto.HeartbeatPayload{MACs: macs, LANIPs: lanIPs}, now); err != nil {
		t.Fatalf("update metrics: %v", err)
	}
}

func postWake(t *testing.T, h *Hub, targetID string) (int, map[string]any) {
	t.Helper()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/agents/"+targetID+"/wake", strings.NewReader("{}"))
	h.handleWake(rec, req, targetID)
	var body map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	return rec.Code, body
}

// TestWakeUnknownTarget: waking a machine the hub has never seen → 404.
func TestWakeUnknownTarget(t *testing.T) {
	h := testHub(t)
	code, body := postWake(t, h, "ghost")
	if code != http.StatusNotFound {
		t.Fatalf("status=%d want 404 (body=%v)", code, body)
	}
}

// TestWakeNoMAC: a known target with no last-known MAC can't be woken → 400 with
// a clear reason (not a silent no-op).
func TestWakeNoMAC(t *testing.T) {
	h := testHub(t)
	persistAgent(t, h, "studio", nil, []string{"192.168.1.50/24"})
	code, body := postWake(t, h, "studio")
	if code != http.StatusBadRequest {
		t.Fatalf("status=%d want 400 (body=%v)", code, body)
	}
	if body["ok"] != false {
		t.Fatalf("expected ok=false, got %v", body["ok"])
	}
}

// TestWakeNoLiveRelay: a known, MAC'd, offline target but NO live agent at all →
// 503 with the explicit no-relay reason — the core "fail loudly" guarantee.
func TestWakeNoLiveRelay(t *testing.T) {
	h := testHub(t)
	persistAgent(t, h, "studio", []string{"aa:bb:cc:dd:ee:ff"}, []string{"192.168.1.50/24"})
	code, body := postWake(t, h, "studio")
	if code != http.StatusServiceUnavailable {
		t.Fatalf("status=%d want 503 (body=%v)", code, body)
	}
	if body["ok"] != false {
		t.Fatalf("expected ok=false, got %v", body["ok"])
	}
	if msg, _ := body["error"].(string); msg == "" {
		t.Fatalf("expected a non-empty no-relay reason")
	}
}

// postPower drives handlePower with a raw JSON body, as a privileged caller.
func postPower(t *testing.T, h *Hub, targetID, body string) int {
	t.Helper()
	rec := httptest.NewRecorder()
	req := privileged(httptest.NewRequest(http.MethodPost, "/api/agents/"+targetID+"/power", strings.NewReader(body)))
	h.handlePower(rec, req, targetID)
	return rec.Code
}

// TestPowerBadAction: a bogus power action → 400 before any agent round-trip.
func TestPowerBadAction(t *testing.T) {
	h := testHub(t)
	for _, body := range []string{
		`{"action":"explode"}`,
		`{"action":""}`,
		`{"action":"hibernate"}`,
		`{"action":"Reboot"}`, // wire values are lowercase
	} {
		if code := postPower(t, h, "studio", body); code != http.StatusBadRequest {
			t.Errorf("body %s: status=%d want 400", body, code)
		}
	}
}

// TestPowerAcceptsKnownActions: sleep/reboot/shutdown all pass validation and go
// on to the agent round-trip. No agent is connected in this hub, so the expected
// outcome is the round-trip's 404 — proving the request got PAST the 400 gate
// rather than being rejected as an unknown action.
func TestPowerAcceptsKnownActions(t *testing.T) {
	h := testHub(t)
	for _, action := range []string{"sleep", "reboot", "shutdown"} {
		code := postPower(t, h, "studio", `{"action":"`+action+`"}`)
		if code == http.StatusBadRequest {
			t.Errorf("action %q rejected as invalid (400)", action)
		}
		if code != http.StatusNotFound {
			t.Errorf("action %q: status=%d want 404 (no such agent connected)", action, code)
		}
	}
}

// TestPowerSurroundingWhitespace: the hub trims the action before validating, so
// a client that sends " reboot " is honoured rather than 400'd.
func TestPowerSurroundingWhitespace(t *testing.T) {
	h := testHub(t)
	if code := postPower(t, h, "studio", `{"action":"  reboot  "}`); code == http.StatusBadRequest {
		t.Fatalf("whitespace-padded action rejected as invalid")
	}
}
