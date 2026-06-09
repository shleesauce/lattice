//go:build windows

package agent

import (
	"context"
	"fmt"
	"os/exec"

	"github.com/shleesauce/lattice/internal/proto"
)

// powerCmd is one OS power command (argv) the agent shells out to.
type powerCmd struct {
	name string
	args []string
}

func (c powerCmd) run(ctx context.Context) error {
	return exec.CommandContext(ctx, c.name, c.args...).Run()
}

// powerCommand maps a PowerAction to the Windows command. Sleep uses
// rundll32 powrprof (does NOT hibernate when hibernation is off); shutdown uses
// shutdown.exe. A failure (e.g. insufficient privilege) surfaces as result Error.
func powerCommand(action proto.PowerAction) (powerCmd, error) {
	switch action {
	case proto.PowerSleep:
		return powerCmd{"rundll32.exe", []string{"powrprof.dll,SetSuspendState", "0,1,0"}}, nil
	case proto.PowerShutdown:
		return powerCmd{"shutdown.exe", []string{"/s", "/t", "0"}}, nil
	}
	return powerCmd{}, fmt.Errorf("unknown power action %q", action)
}
