//go:build darwin || linux

package agent

import (
	"context"
	"fmt"
	"os/exec"
	"runtime"

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

// powerCommand maps a PowerAction to the platform command. macOS uses pmset
// (sleep) / shutdown; Linux uses systemctl. These typically require privilege —
// the agent runs as the login user, so sleep generally works unprivileged on
// macOS, while shutdown/Linux suspend may need the agent to run as root or with
// a polkit rule. A permission failure surfaces back as the result Error.
func powerCommand(action proto.PowerAction) (powerCmd, error) {
	switch runtime.GOOS {
	case "darwin":
		switch action {
		case proto.PowerSleep:
			return powerCmd{"pmset", []string{"sleepnow"}}, nil
		case proto.PowerShutdown:
			return powerCmd{"shutdown", []string{"-h", "now"}}, nil
		}
	case "linux":
		switch action {
		case proto.PowerSleep:
			return powerCmd{"systemctl", []string{"suspend"}}, nil
		case proto.PowerShutdown:
			return powerCmd{"systemctl", []string{"poweroff"}}, nil
		}
	}
	return powerCmd{}, fmt.Errorf("power %q not supported on %s", action, runtime.GOOS)
}
