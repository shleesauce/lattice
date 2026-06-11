package agent

import (
	"context"
	"testing"
	"time"

	"github.com/shleesauce/lattice/internal/proto"
	"github.com/shleesauce/lattice/internal/update"
)

// decodeUpdateResult pulls the next UpdateResultPayload off the outbound channel.
func decodeUpdateResult(t *testing.T, outbound <-chan []byte) proto.UpdateResultPayload {
	t.Helper()
	select {
	case raw := <-outbound:
		env, err := proto.Decode(raw)
		if err != nil {
			t.Fatalf("decode frame: %v", err)
		}
		var res proto.UpdateResultPayload
		if err := proto.As(env, &res); err != nil {
			t.Fatalf("as UpdateResultPayload: %v", err)
		}
		return res
	default:
		t.Fatal("expected an UpdateResultPayload frame on outbound, got none")
		return proto.UpdateResultPayload{}
	}
}

func stubUpdate(t *testing.T) {
	t.Helper()
	oApply, oLabel, oRestart, oGrace := updateApply, updateServiceLabel, updateRestart, restartGrace
	t.Cleanup(func() {
		updateApply, updateServiceLabel, updateRestart, restartGrace = oApply, oLabel, oRestart, oGrace
	})
	restartGrace = time.Millisecond
}

// The v0.1.6 fix: the agent must ack the hub BEFORE it restarts its service, or the
// restart tears the connection down before the frame ships and the hub times out on
// a perfectly good update. This asserts the ack frame is already queued at the moment
// restart runs.
func TestHandleUpdateAcksBeforeRestart(t *testing.T) {
	stubUpdate(t)
	updateApply = func(context.Context, update.Options) (string, error) { return "base", nil }
	updateServiceLabel = func() string { return "sh.lattice.agent" }

	outbound := make(chan []byte, 4)
	ackedBeforeRestart := false
	var restartLabel string
	updateRestart = func(label string) error {
		restartLabel = label
		// Non-blocking peek: the ack must already be in the buffer at restart time.
		if len(outbound) > 0 {
			ackedBeforeRestart = true
		}
		return nil
	}

	handleUpdate(context.Background(), proto.UpdatePayload{ReqID: "r1", Version: "v9.9.9"}, outbound)

	if !ackedBeforeRestart {
		t.Fatal("restart ran before the ack was queued — the v0.1.5 cascade race is NOT fixed")
	}
	if restartLabel != "sh.lattice.agent" {
		t.Fatalf("restart label = %q, want sh.lattice.agent", restartLabel)
	}
	res := decodeUpdateResult(t, outbound)
	if !res.OK || res.ReqID != "r1" || res.Restarted != "sh.lattice.agent" {
		t.Fatalf("ack = %+v, want OK reqID=r1 restarted=sh.lattice.agent", res)
	}
}

// No installed service → ack still sent (OK, no label), and restart is never called.
func TestHandleUpdateNoServiceStillAcks(t *testing.T) {
	stubUpdate(t)
	updateApply = func(context.Context, update.Options) (string, error) { return "base", nil }
	updateServiceLabel = func() string { return "" }
	restarted := false
	updateRestart = func(string) error { restarted = true; return nil }

	outbound := make(chan []byte, 4)
	handleUpdate(context.Background(), proto.UpdatePayload{ReqID: "r2"}, outbound)

	if restarted {
		t.Fatal("restart should not run when no service is installed")
	}
	res := decodeUpdateResult(t, outbound)
	if !res.OK || res.Restarted != "" {
		t.Fatalf("ack = %+v, want OK with empty restarted", res)
	}
}

// A fail-closed Apply error is reported and the agent never restarts (still on the
// old binary).
func TestHandleUpdateApplyErrorReportsAndDoesNotRestart(t *testing.T) {
	stubUpdate(t)
	updateApply = func(context.Context, update.Options) (string, error) {
		return "", context.DeadlineExceeded
	}
	restarted := false
	updateServiceLabel = func() string { return "sh.lattice.agent" }
	updateRestart = func(string) error { restarted = true; return nil }

	outbound := make(chan []byte, 4)
	handleUpdate(context.Background(), proto.UpdatePayload{ReqID: "r3"}, outbound)

	if restarted {
		t.Fatal("must NOT restart when Apply failed (binary not swapped)")
	}
	res := decodeUpdateResult(t, outbound)
	if res.OK || res.Error == "" {
		t.Fatalf("ack = %+v, want OK=false with an error", res)
	}
}
