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
	oExit, oGoos, oPM2 := exitAfterRestart, goos, updateUnderPM2
	t.Cleanup(func() {
		updateApply, updateServiceLabel, updateRestart, restartGrace = oApply, oLabel, oRestart, oGrace
		exitAfterRestart, goos, updateUnderPM2 = oExit, oGoos, oPM2
	})
	restartGrace = time.Millisecond
	// Default: non-windows + a no-op exit, so existing tests never call os.Exit.
	goos = "darwin"
	// Stubbed false so the suite is hermetic: run from a PM2-spawned shell the real
	// update.UnderPM2() would see pm_id in the environment and send the no-service
	// tests down the exit path.
	updateUnderPM2 = func() bool { return false }
	exitAfterRestart = func() { t.Fatal("exitAfterRestart called unexpectedly (non-windows path)") }
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

// The v0.1.8 fix: on Windows, schtasks /End+/Run does NOT kill the calling process,
// so the agent must exit itself AFTER a successful restart or the old + new agents
// duel under one id (reconnect storm). Assert it acks, restarts, THEN self-exits.
func TestHandleUpdateWindowsSelfExitsAfterRestart(t *testing.T) {
	stubUpdate(t)
	goos = "windows"
	updateApply = func(context.Context, update.Options) (string, error) { return "base", nil }
	updateServiceLabel = func() string { return "LatticeAgent" }
	restarted := false
	updateRestart = func(string) error { restarted = true; return nil }
	exited := false
	exitAfterRestart = func() { exited = true }

	outbound := make(chan []byte, 4)
	handleUpdate(context.Background(), proto.UpdatePayload{ReqID: "rw", Version: "v9.9.9"}, outbound)

	if !restarted {
		t.Fatal("restart should have been called")
	}
	if !exited {
		t.Fatal("windows agent must self-exit after restart, else the old process orphans → storm")
	}
	res := decodeUpdateResult(t, outbound)
	if !res.OK || res.Restarted != "LatticeAgent" {
		t.Fatalf("ack = %+v, want OK restarted=LatticeAgent", res)
	}
}

// If the Windows restart FAILS, the agent must NOT self-exit — the new instance
// never started, so exiting would leave the box with no agent until next boot.
func TestHandleUpdateWindowsRestartErrorDoesNotExit(t *testing.T) {
	stubUpdate(t)
	goos = "windows"
	updateApply = func(context.Context, update.Options) (string, error) { return "base", nil }
	updateServiceLabel = func() string { return "LatticeAgent" }
	updateRestart = func(string) error { return context.DeadlineExceeded }
	exited := false
	exitAfterRestart = func() { exited = true }

	outbound := make(chan []byte, 4)
	handleUpdate(context.Background(), proto.UpdatePayload{ReqID: "rwe"}, outbound)

	if exited {
		t.Fatal("must NOT self-exit when the restart failed (would leave no agent)")
	}
}

// Non-Windows must rely on the service manager's kill (kickstart -k / systemctl
// restart) and never self-exit — the default stub fails the test if it does.
func TestHandleUpdateNonWindowsDoesNotSelfExit(t *testing.T) {
	stubUpdate(t) // goos defaults to darwin; exitAfterRestart fails the test if called
	updateApply = func(context.Context, update.Options) (string, error) { return "base", nil }
	updateServiceLabel = func() string { return "sh.lattice.agent" }
	updateRestart = func(string) error { return nil }

	outbound := make(chan []byte, 4)
	handleUpdate(context.Background(), proto.UpdatePayload{ReqID: "rd", Version: "v9.9.9"}, outbound)
	_ = decodeUpdateResult(t, outbound)
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

// The v0.2.3 fix for the mini-ops agent: PM2 is not a service the agent can
// kickstart (ServiceLabel is ""), but PM2 DOES relaunch an app that exits. Before
// this, the agent on a PM2 host took the "no service to restart" path and stayed
// on the OLD binary until a human ran `pm2 restart` — which is how mini-ops
// shipped v0.2.2 twice. Assert it still acks OK first, then self-exits.
func TestHandleUpdateNoServiceExitsUnderPM2(t *testing.T) {
	stubUpdate(t)
	updateApply = func(context.Context, update.Options) (string, error) { return "base", nil }
	updateServiceLabel = func() string { return "" } // PM2 installs no launchd label
	updateUnderPM2 = func() bool { return true }
	restarted := false
	updateRestart = func(string) error { restarted = true; return nil }

	exited := false
	ackedBeforeExit := false
	outbound := make(chan []byte, 4)
	exitAfterRestart = func() {
		exited = true
		// The ack must already be on the wire: once we exit, PM2 relaunches a fresh
		// process that knows nothing about this reqID.
		if len(outbound) > 0 {
			ackedBeforeExit = true
		}
	}

	handleUpdate(context.Background(), proto.UpdatePayload{ReqID: "r-pm2", Version: "v9.9.9"}, outbound)

	if !exited {
		t.Fatal("agent did not exit under PM2 — it will keep running the OLD binary")
	}
	if !ackedBeforeExit {
		t.Error("agent exited before the ack was queued; the hub will record a timeout")
	}
	if restarted {
		t.Error("updateRestart was called under PM2 — there is no service label to restart")
	}
	res := decodeUpdateResult(t, outbound)
	if !res.OK || res.ReqID != "r-pm2" {
		t.Fatalf("ack = %+v, want OK reqID=r-pm2", res)
	}
	// No launchd/systemd label was used, so the ack must not claim one.
	if res.Restarted != "" {
		t.Errorf("Restarted = %q, want empty (PM2 restarted it, not a Lattice-managed service)", res.Restarted)
	}
}

// The inverse guard: with no service AND no PM2 (bare foreground process), the
// agent must NOT exit — exiting there would take the agent off the mesh entirely
// with nothing to bring it back.
func TestHandleUpdateNoServiceNoPM2DoesNotExit(t *testing.T) {
	stubUpdate(t) // exitAfterRestart t.Fatal's if called; updateUnderPM2 stubbed false
	updateApply = func(context.Context, update.Options) (string, error) { return "base", nil }
	updateServiceLabel = func() string { return "" }
	updateRestart = func(string) error { t.Fatal("restart called with no service label"); return nil }

	outbound := make(chan []byte, 4)
	handleUpdate(context.Background(), proto.UpdatePayload{ReqID: "r-bare"}, outbound)

	if res := decodeUpdateResult(t, outbound); !res.OK {
		t.Fatalf("ack = %+v, want OK", res)
	}
}
