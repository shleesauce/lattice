package agent

import (
	"context"
	"log"
	"time"

	"github.com/shleesauce/lattice/internal/proto"
	"github.com/shleesauce/lattice/internal/update"
)

// handleUpdate pulls+verifies+swaps the release binary on this agent, reports the
// outcome back to the hub, then restarts the agent's own service so the new build
// takes over. The result frame is sent BEFORE the restart kicks: the restart
// (launchctl kickstart / systemctl restart / schtasks) tears down this process,
// so a result sent after it would never make it onto the wire and the hub's
// lockstep round-trip would time out even on a perfectly successful update.
//
// Verification is FAIL CLOSED inside update.Apply (never Insecure here): a bad or
// unreachable SHA256SUMS aborts with the agent STILL on its old binary, and we
// report that error so the hub can surface a partial-fleet result instead of
// silently restarting onto an unverified binary.
func handleUpdate(ctx context.Context, p proto.UpdatePayload, outbound chan<- []byte) {
	result := proto.UpdateResultPayload{ReqID: p.ReqID}

	if _, err := update.Apply(ctx, update.Options{Base: p.Base}); err != nil {
		result.Error = err.Error()
		log.Printf("agent: update failed: %v", err)
		sendFrame(ctx, outbound, proto.TypeUpdateResult, result)
		return
	}

	result.OK = true
	result.Restarted = update.Restart()
	log.Printf("agent: update applied (%s); restarting service %q", p.Version, result.Restarted)

	// Tell the hub we're good BEFORE the restart yanks the process. Give the
	// writer goroutine a beat to flush the frame onto the socket; the restart
	// follows immediately after.
	sendFrame(ctx, outbound, proto.TypeUpdateResult, result)
	time.Sleep(250 * time.Millisecond)

	// If no service was found to restart, the swapped binary still applies the
	// next time the agent starts — nothing more to do here.
	if result.Restarted == "" {
		log.Printf("agent: no installed Lattice service to restart; new binary applies on next start")
	}
}
