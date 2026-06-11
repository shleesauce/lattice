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

// TestPowerBadAction: a bogus power action → 400 before any agent round-trip.
func TestPowerBadAction(t *testing.T) {
	h := testHub(t)
	rec := httptest.NewRecorder()
	req := privileged(httptest.NewRequest(http.MethodPost, "/api/agents/studio/power", strings.NewReader(`{"action":"explode"}`)))
	h.handlePower(rec, req, "studio")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status=%d want 400", rec.Code)
	}
}
