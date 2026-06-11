package agent

import (
	"context"
	"log"
	"os"
	"runtime"
	"time"

	"github.com/shleesauce/lattice/internal/proto"
	"github.com/shleesauce/lattice/internal/update"
)

// restartGrace is how long the agent waits after acking the hub before it actually
// restarts its service. The ack must be on the wire AND processed by the hub before
// the restart (kickstart -k / systemctl restart / schtasks) tears this process down
// — otherwise the hub's lockstep round-trip times out even on a perfect update (the
// v0.1.5 cascade race). Mirrors the hub's own 750ms pre-restart delay in handleUpdate.
// A var (not const) so tests can shrink it.
var restartGrace = 750 * time.Millisecond

// Indirection points for the update side effects, so a test can assert the critical
// ordering (ack BEFORE restart, self-exit AFTER restart on Windows) without real
// downloads, service restarts, or actually calling os.Exit.
var (
	updateApply        = update.Apply
	updateServiceLabel = update.ServiceLabel
	updateRestart      = update.RestartByLabel
	// exitAfterRestart terminates THIS process after a successful Windows service
	// restart — see handleUpdate. A var so a test can assert it ran without exiting.
	exitAfterRestart = func() { os.Exit(0) }
	// goos is runtime.GOOS, overridable in tests to exercise the Windows path.
	goos = runtime.GOOS
)

// handleUpdate pulls+verifies+swaps the release binary on this agent, reports the
// outcome back to the hub, THEN — only after the ack has had time to land — restarts
// the agent's own service so the new build takes over.
//
// Ordering matters: update.ServiceLabel() only DETECTS the service (no restart), so
// we can put the label in the result frame and ack the hub FIRST; the real restart
// (update.RestartByLabel) happens after restartGrace. Doing it the other way (restart
// then ack, as v0.1.5 did) let the restart kill the process before the frame flushed,
// so the hub saw a timeout on a successful update.
//
// Verification is FAIL CLOSED inside update.Apply (never Insecure here): a bad or
// unreachable SHA256SUMS aborts with the agent STILL on its old binary, and we
// report that error so the hub can surface a partial-fleet result instead of
// silently restarting onto an unverified binary.
func handleUpdate(ctx context.Context, p proto.UpdatePayload, outbound chan<- []byte) {
	result := proto.UpdateResultPayload{ReqID: p.ReqID}

	if _, err := updateApply(ctx, update.Options{Base: p.Base}); err != nil {
		result.Error = err.Error()
		log.Printf("agent: update failed: %v", err)
		sendFrame(ctx, outbound, proto.TypeUpdateResult, result)
		return
	}

	// Detect (don't restart) the service so we can name it in the ack.
	label := updateServiceLabel()
	result.OK = true
	result.Restarted = label

	// Ack the hub BEFORE restarting, then give the frame time to flush + be
	// processed before the restart yanks the process out from under the socket.
	sendFrame(ctx, outbound, proto.TypeUpdateResult, result)

	if label == "" {
		// No service to restart — the swapped binary applies on next start.
		log.Printf("agent: update applied (%s); no installed Lattice service to restart; new binary applies on next start", p.Version)
		return
	}

	log.Printf("agent: update applied (%s); restarting service %q after ack", p.Version, label)
	time.Sleep(restartGrace)
	if err := updateRestart(label); err != nil {
		// Restart failed, but the binary is already swapped — it applies on next
		// start. Do NOT self-exit: the new instance didn't start, so exiting would
		// leave the box with no agent until its next boot/logon.
		log.Printf("agent: restart of %q failed: %v; new binary applies on next start", label, err)
		return
	}

	// On Windows the restart (schtasks /End + /Run) starts a NEW agent but does NOT
	// kill THIS process: the wscript-wrapped lattice.exe that initiated the call
	// survives, so the old + new agents duel under one id → reconnect storm (the
	// v0.1.7 cascade bug). macOS kickstart -k / linux systemctl restart SIGKILL the
	// old process for us; Windows has no equivalent, so exit explicitly — the
	// scheduled task's fresh instance is left as the sole agent. The ack already had
	// restartGrace to flush, so exiting now is safe.
	if goos == "windows" {
		log.Printf("agent: update applied (%s); exiting old process so the restarted service is the sole agent", p.Version)
		exitAfterRestart()
	}
}
