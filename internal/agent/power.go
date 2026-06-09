package agent

import (
	"context"
	"fmt"
	"time"

	"github.com/shleesauce/lattice/internal/proto"
)

// powerControl sleeps or shuts down the agent's OWN machine on hub request, then
// pushes a power_control_result correlated by ReqID. This is the last frame the
// agent sends before it goes offline — closing the unattended loop (wake → work →
// sleep). Waking a slept box back up is WoL (TypeWake from a LAN peer), never a
// frame to this agent (it isn't connected while asleep).
func powerControl(ctx context.Context, p proto.PowerControlPayload, outbound chan<- []byte) {
	result := proto.PowerControlResultPayload{ReqID: p.ReqID, Action: string(p.Action)}

	if p.Action != proto.PowerSleep && p.Action != proto.PowerShutdown {
		result.Error = fmt.Sprintf("unknown power action %q", p.Action)
		sendFrame(ctx, outbound, proto.TypePowerControlResult, result)
		return
	}

	cmd, err := powerCommand(p.Action)
	if err != nil {
		result.Error = err.Error()
		sendFrame(ctx, outbound, proto.TypePowerControlResult, result)
		return
	}

	// Ack BEFORE executing: sleep/shutdown can sever the connection mid-command,
	// so the hub must get the "issued" frame first. A short grace window lets the
	// frame flush over the WebSocket before the OS suspends networking.
	result.OK = true
	sendFrame(ctx, outbound, proto.TypePowerControlResult, result)

	go func() {
		time.Sleep(500 * time.Millisecond)
		// Detach from the request context: the suspend command itself can outlive
		// (or kill) the connection that carried the request.
		runCtx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		_ = cmd.run(runCtx)
	}()
}
