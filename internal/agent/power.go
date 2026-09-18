package agent

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"os/user"
	"runtime"
	"strings"
	"time"

	"github.com/shleesauce/lattice/internal/proto"
)

// powerRunTimeout bounds the whole candidate chain. A reboot normally kills this
// process long before it elapses; the timeout only matters when the command fails.
const powerRunTimeout = 20 * time.Second

// powerAckGrace is how long the agent waits after acking OK before it executes, so
// the frame is on the wire before the OS tears networking down.
var powerAckGrace = 500 * time.Millisecond

// powerCmd is one candidate OS power command (argv) the agent shells out to.
type powerCmd struct {
	name string
	args []string
}

func (c powerCmd) String() string {
	if len(c.args) == 0 {
		return c.name
	}
	return c.name + " " + strings.Join(c.args, " ")
}

// run executes the candidate and returns its combined output, so a failure can be
// reported with the OS's own words ("shutdown: NOT super-user") instead of a bare
// "exit status 1".
func (c powerCmd) run(ctx context.Context) ([]byte, error) {
	return exec.CommandContext(ctx, c.name, c.args...).CombinedOutput()
}

// isSudo reports whether this candidate is the non-interactive sudo wrapper.
func (c powerCmd) isSudo() bool { return c.name == "sudo" }

// sudoWrap prefixes a command with a NON-INTERACTIVE sudo. `sudo -n` never
// prompts: with no usable credential it exits 1 immediately with "a password is
// required". That is the whole reason this is safe to run from a daemon with no
// tty — plain `sudo` could block on a prompt nobody can answer (D39's objection),
// `sudo -n` structurally cannot. See D40.
func sudoWrap(c powerCmd) powerCmd {
	return powerCmd{"sudo", append([]string{"-n", c.name}, c.args...)}
}

// powerCommandCandidates returns the ORDERED argv list to try for an action on
// goos, given whether this process is root. The first candidate that exits 0 wins.
//
//   - sleep never needs privilege (macOS pmset, Linux logind/polkit), so it is
//     always the bare command.
//   - reboot/shutdown on darwin+linux as a non-root user get `sudo -n <cmd>` first
//     and the bare command as the fallback for hosts where the OS allows it without
//     sudo (Linux polkit) or where sudo is absent.
//   - as root the bare command is the only candidate: wrapping it in sudo would add
//     a dependency for no gain.
//   - windows never uses sudo; shutdown.exe already works for an admin token and
//     reports its own error otherwise.
//
// Pure: it only builds argv, it never runs anything.
func powerCommandCandidates(goos string, action proto.PowerAction, isRoot bool) ([]powerCmd, error) {
	var base powerCmd
	switch goos {
	case "darwin":
		switch action {
		case proto.PowerSleep:
			base = powerCmd{"pmset", []string{"sleepnow"}}
		case proto.PowerReboot:
			base = powerCmd{"shutdown", []string{"-r", "now"}}
		case proto.PowerShutdown:
			base = powerCmd{"shutdown", []string{"-h", "now"}}
		default:
			return nil, fmt.Errorf("power %q not supported on %s", action, goos)
		}
	case "linux":
		switch action {
		case proto.PowerSleep:
			base = powerCmd{"systemctl", []string{"suspend"}}
		case proto.PowerReboot:
			base = powerCmd{"systemctl", []string{"reboot"}}
		case proto.PowerShutdown:
			base = powerCmd{"systemctl", []string{"poweroff"}}
		default:
			return nil, fmt.Errorf("power %q not supported on %s", action, goos)
		}
	case "windows":
		switch action {
		case proto.PowerSleep:
			return []powerCmd{{"rundll32.exe", []string{"powrprof.dll,SetSuspendState", "0,1,0"}}}, nil
		case proto.PowerReboot:
			return []powerCmd{{"shutdown.exe", []string{"/r", "/t", "0"}}}, nil
		case proto.PowerShutdown:
			return []powerCmd{{"shutdown.exe", []string{"/s", "/t", "0"}}}, nil
		default:
			return nil, fmt.Errorf("power %q not supported on %s", action, goos)
		}
	default:
		return nil, fmt.Errorf("power %q not supported on %s", action, goos)
	}

	if isRoot || action == proto.PowerSleep {
		return []powerCmd{base}, nil
	}
	return []powerCmd{sudoWrap(base), base}, nil
}

// needsSudoProbe reports whether the pre-flight should spend a `sudo -n true` to
// learn if this user can escalate. Only the unix halt/restart paths care.
func needsSudoProbe(goos string, action proto.PowerAction, isRoot bool) bool {
	if isRoot || (goos != "darwin" && goos != "linux") {
		return false
	}
	return action == proto.PowerReboot || action == proto.PowerShutdown
}

// powerPreflight returns "" when the action is worth attempting, or the operator-
// facing reason it CANNOT succeed — in which case the caller answers ok:false and
// runs nothing.
//
// The only case that is provably hopeless is macOS: shutdown(8) demands euid 0 and
// there is no polkit-style escape hatch, so a non-root user with no NOPASSWD sudo
// will always get "shutdown: NOT super-user" — the exact v0.2.2 bug, which the old
// code reported as ok:true. Linux is deliberately NOT pre-failed: systemd-logind
// grants reboot/poweroff to a local session over polkit, so an unprivileged
// `systemctl reboot` genuinely can succeed and pre-failing it would be a lie in the
// other direction. Windows attempts and lets shutdown.exe report its own refusal.
func powerPreflight(goos string, action proto.PowerAction, isRoot, sudoOK bool, username string) string {
	if isRoot || sudoOK || action == proto.PowerSleep {
		return ""
	}
	if goos != "darwin" {
		return ""
	}
	if action != proto.PowerReboot && action != proto.PowerShutdown {
		return ""
	}
	who := username
	if who == "" {
		who = "the agent's user"
	}
	return fmt.Sprintf(
		"%s needs root: user %s is not root and has no NOPASSWD sudo rule, so macOS shutdown would fail with \"NOT super-user\". Nothing was executed. "+
			"Fix on that machine: echo '%s ALL=(ALL) NOPASSWD: /sbin/shutdown' | sudo tee /etc/sudoers.d/lattice-power && sudo chmod 440 /etc/sudoers.d/lattice-power",
		action, who, who)
}

// sudoRefused reports whether sudo ITSELF declined (no credential, no tty, not in
// sudoers) as opposed to the wrapped command running and failing. Only a refusal
// justifies retrying the bare, unprivileged candidate.
func sudoRefused(out string) bool {
	l := strings.ToLower(out)
	for _, needle := range []string{
		"a password is required",
		"a terminal is required",
		"no tty present",
		"is not in the sudoers file",
		"may not run",
		"command not allowed",
		"sorry, user",
		"no askpass program",
	} {
		if strings.Contains(l, needle) {
			return true
		}
	}
	return false
}

// sudoNonInteractiveOK probes escalation with `sudo -n true`. A var so tests can
// stub it. Bounded by its own short deadline; `-n` cannot prompt, so it cannot hang.
var sudoNonInteractiveOK = func(ctx context.Context) bool {
	probeCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	return exec.CommandContext(probeCtx, "sudo", "-n", "true").Run() == nil
}

// currentUsername is the agent's OS user, for the pre-flight message. Falls back to
// $USER / $USERNAME when os/user is unavailable (static cross-builds).
func currentUsername() string {
	if u, err := user.Current(); err == nil && u.Username != "" {
		return u.Username
	}
	if n := os.Getenv("USER"); n != "" {
		return n
	}
	return os.Getenv("USERNAME")
}

// runPowerCandidates tries each candidate in order and returns nil on the first
// success, or an error naming every attempt and the OS's own message.
func runPowerCandidates(ctx context.Context, cands []powerCmd) error {
	if len(cands) == 0 {
		return errors.New("no power command to run")
	}
	failures := make([]string, 0, len(cands))
	for i, c := range cands {
		out, err := c.run(ctx)
		if err == nil {
			return nil
		}
		failures = append(failures, fmt.Sprintf("%q: %v%s", c.String(), err, quoteOutput(out)))
		// Fall through to the unprivileged fallback only when sudo refused us or is
		// missing. If sudo ran the command and the COMMAND failed, retrying it bare
		// can only fail the same way (and would be a second reboot attempt if the
		// first one half-took).
		if i < len(cands)-1 && c.isSudo() &&
			!sudoRefused(string(out)) && !errors.Is(err, exec.ErrNotFound) {
			break
		}
	}
	return errors.New(strings.Join(failures, "; "))
}

// quoteOutput renders a command's combined output for an error string, or "" when
// it said nothing.
func quoteOutput(out []byte) string {
	s := strings.TrimSpace(string(out))
	if s == "" {
		return ""
	}
	return " — " + s
}

// powerControl sleeps, reboots, or shuts down the agent's OWN machine on hub
// request, then pushes a power_control_result correlated by ReqID. This is the
// last frame the agent sends before it goes offline — closing the unattended loop
// (wake → work → sleep). Waking a slept box back up is WoL (TypeWake from a LAN
// peer), never a frame to this agent (it isn't connected while asleep); a reboot
// is the one action the agent re-dials from on its own once the OS service starts.
//
// ok=true means ISSUED, and is only sent once a pre-flight says the command can
// actually run. v0.2.2 acked ok=true unconditionally and then threw the command's
// error away (`_ = cmd.run(...)`), so a macOS reboot that died on "NOT super-user"
// was reported — and audited — as a success while the box stayed up. Now:
//
//  1. pre-flight (euid, `sudo -n true`) BEFORE the ack; a hopeless action answers
//     ok=false with the fix and executes nothing;
//  2. the ack-before-action ordering is kept only past that gate, because from there
//     the process really is expected to die mid-command;
//  3. a post-ack failure is logged AND pushed as a late ok=false frame. The hub drops
//     a late frame from its pending map (the HTTP round-trip has already answered)
//     but audits it as power_control_late, so the audit trail ends up truthful.
func powerControl(ctx context.Context, p proto.PowerControlPayload, outbound chan<- []byte) {
	result := proto.PowerControlResultPayload{ReqID: p.ReqID, Action: string(p.Action)}

	if !p.Action.Valid() {
		result.Error = fmt.Sprintf("unknown power action %q", p.Action)
		log.Printf("power: %s", result.Error)
		sendFrame(ctx, outbound, proto.TypePowerControlResult, result)
		return
	}

	isRoot := os.Geteuid() == 0
	cands, err := powerCommandCandidates(runtime.GOOS, p.Action, isRoot)
	if err != nil {
		result.Error = err.Error()
		log.Printf("power: %v", err)
		sendFrame(ctx, outbound, proto.TypePowerControlResult, result)
		return
	}

	sudoOK := false
	if needsSudoProbe(runtime.GOOS, p.Action, isRoot) {
		sudoOK = sudoNonInteractiveOK(ctx)
		log.Printf("power: pre-flight %s user=%s root=%v sudo-n=%v", p.Action, currentUsername(), isRoot, sudoOK)
	}
	if msg := powerPreflight(runtime.GOOS, p.Action, isRoot, sudoOK, currentUsername()); msg != "" {
		result.Error = msg
		log.Printf("power: refusing %s without executing anything — %s", p.Action, msg)
		sendFrame(ctx, outbound, proto.TypePowerControlResult, result)
		return
	}

	// Ack BEFORE executing: sleep/reboot/shutdown can sever the connection
	// mid-command, so the hub must get the "issued" frame first. A short grace
	// window lets the frame flush over the WebSocket before the OS tears down
	// networking.
	result.OK = true
	sendFrame(ctx, outbound, proto.TypePowerControlResult, result)

	go func() {
		time.Sleep(powerAckGrace)
		// Detach from the request context: the suspend command itself can outlive
		// (or kill) the connection that carried the request.
		runCtx, cancel := context.WithTimeout(context.Background(), powerRunTimeout)
		defer cancel()
		if err := runPowerCandidates(runCtx, cands); err != nil {
			log.Printf("power: %s FAILED after ack — this machine is STILL UP: %v", p.Action, err)
			lateCtx, lateCancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer lateCancel()
			sendFrame(lateCtx, outbound, proto.TypePowerControlResult, proto.PowerControlResultPayload{
				ReqID: p.ReqID, Action: string(p.Action), Error: err.Error(),
			})
			return
		}
		log.Printf("power: %s issued", p.Action)
	}()
}
