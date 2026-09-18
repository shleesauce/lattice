package hub

import (
	"encoding/json"
	"testing"

	"github.com/shleesauce/lattice/internal/proto"
)

// TestResolvePendingReportsLateFrames: the read loop has to be able to tell a
// result that someone is waiting for from one that arrived after the round-trip
// closed. Power is the case that needs it — it acks before it acts, so the only
// moment the command's real outcome is known is always "late".
func TestResolvePendingReportsLateFrames(t *testing.T) {
	r := NewRegistry()
	ch := r.registerPending("req-live")

	if !r.resolvePending("req-live", proto.Envelope{Type: proto.TypePowerControlResult}) {
		t.Fatal("a registered reqId reported as late")
	}
	select {
	case <-ch:
	default:
		t.Fatal("the waiting channel never received the envelope")
	}
	// Same reqId again: the waiter is gone, so this one is late.
	if r.resolvePending("req-live", proto.Envelope{Type: proto.TypePowerControlResult}) {
		t.Error("a second frame for the same reqId reported as delivered")
	}
	// A reqId nobody ever waited on is late too — and must not panic.
	if r.resolvePending("req-never", proto.Envelope{Type: proto.TypePowerControlResult}) {
		t.Error("an unknown reqId reported as delivered")
	}
}

// TestAuditLatePowerRecordsTheFailure is the durable half of the v0.2.2 fix. The
// operator's HTTP response already said ok=true (ack-before-action), so when the
// command then fails the ONLY place the truth can still land is audit_log. Before
// this, the late frame was dropped and the log kept a success row for a machine
// that never went down.
func TestAuditLatePowerRecordsTheFailure(t *testing.T) {
	st := testStore(t)
	h := &Hub{store: st, registry: NewRegistry()}

	h.auditLatePower("agent-emu", proto.PowerControlResultPayload{
		ReqID:  "req-9",
		Action: "reboot",
		OK:     false,
		Error:  `"sudo -n shutdown -r now": exit status 1 — shutdown: NOT super-user`,
	})

	var eventType, toolName, detailJSON string
	row := st.db.QueryRow(`SELECT event_type, tool_name, detail_json FROM audit_log
		WHERE agent_id = ? ORDER BY id DESC LIMIT 1`, "agent-emu")
	if err := row.Scan(&eventType, &toolName, &detailJSON); err != nil {
		t.Fatalf("no audit row written: %v", err)
	}
	if eventType != "power_control_late" {
		t.Errorf("event_type = %q, want power_control_late", eventType)
	}
	if toolName != "reboot" {
		t.Errorf("tool_name = %q, want reboot", toolName)
	}

	var detail struct {
		OK    bool   `json:"ok"`
		Error string `json:"error"`
		Late  bool   `json:"late"`
	}
	if err := json.Unmarshal([]byte(detailJSON), &detail); err != nil {
		t.Fatalf("detail_json is not JSON: %v", err)
	}
	if detail.OK {
		t.Error("the late row claims ok=true for a reboot that failed")
	}
	if !detail.Late {
		t.Error("the late row is not marked late")
	}
	if detail.Error == "" {
		t.Error("the late row lost the command's error text")
	}
}
