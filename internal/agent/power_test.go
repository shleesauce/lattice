package agent

import (
	"context"
	"net"
	"os"
	"os/exec"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/shleesauce/lattice/internal/proto"
)

// argv flattens a candidate back to a comparable []string.
func argv(c powerCmd) []string { return append([]string{c.name}, c.args...) }

// argvs flattens a candidate list.
func argvs(cs []powerCmd) [][]string {
	out := make([][]string, 0, len(cs))
	for _, c := range cs {
		out = append(out, argv(c))
	}
	return out
}

// TestPowerCommandKnownActions: sleep, reboot + shutdown all resolve to at least
// one concrete candidate on every OS the agent ships for.
func TestPowerCommandKnownActions(t *testing.T) {
	for _, goos := range []string{"darwin", "linux", "windows"} {
		for _, action := range []proto.PowerAction{proto.PowerSleep, proto.PowerReboot, proto.PowerShutdown} {
			for _, isRoot := range []bool{false, true} {
				cands, err := powerCommandCandidates(goos, action, isRoot)
				if err != nil {
					t.Fatalf("power %q on %s (root=%v): unexpected error %v", action, goos, isRoot, err)
				}
				if len(cands) == 0 {
					t.Fatalf("power %q on %s (root=%v): no candidates", action, goos, isRoot)
				}
				for _, c := range cands {
					if c.name == "" {
						t.Fatalf("power %q on %s (root=%v): empty command name", action, goos, isRoot)
					}
				}
			}
		}
	}
}

// TestPowerCommandCandidatesNonRoot pins the ORDERED argv each action maps to for
// an unprivileged agent — the v0.2.2 bug's exact ground: on macOS the bare
// `shutdown -r now` exits 1 ("NOT super-user"), so `sudo -n` has to come first and
// the bare command stays as the fallback. Table-driven and pure: this function
// only constructs argv, it never runs a command.
func TestPowerCommandCandidatesNonRoot(t *testing.T) {
	want := map[string]map[proto.PowerAction][][]string{
		"darwin": {
			proto.PowerSleep: {{"pmset", "sleepnow"}},
			proto.PowerReboot: {
				{"sudo", "-n", "shutdown", "-r", "now"},
				{"shutdown", "-r", "now"},
			},
			proto.PowerShutdown: {
				{"sudo", "-n", "shutdown", "-h", "now"},
				{"shutdown", "-h", "now"},
			},
		},
		"linux": {
			proto.PowerSleep: {{"systemctl", "suspend"}},
			proto.PowerReboot: {
				{"sudo", "-n", "systemctl", "reboot"},
				{"systemctl", "reboot"},
			},
			proto.PowerShutdown: {
				{"sudo", "-n", "systemctl", "poweroff"},
				{"systemctl", "poweroff"},
			},
		},
		// Windows never gets a sudo candidate — there is no sudo, and shutdown.exe
		// already works for an admin token.
		"windows": {
			proto.PowerSleep:    {{"rundll32.exe", "powrprof.dll,SetSuspendState", "0,1,0"}},
			proto.PowerReboot:   {{"shutdown.exe", "/r", "/t", "0"}},
			proto.PowerShutdown: {{"shutdown.exe", "/s", "/t", "0"}},
		},
	}
	for goos, table := range want {
		for action, wantArgv := range table {
			cands, err := powerCommandCandidates(goos, action, false)
			if err != nil {
				t.Fatalf("power %q on %s: unexpected error %v", action, goos, err)
			}
			if got := argvs(cands); !reflect.DeepEqual(got, wantArgv) {
				t.Errorf("power %q on %s (non-root): got %v want %v", action, goos, got, wantArgv)
			}
		}
	}
}

// TestPowerCommandCandidatesRoot: as root there is exactly ONE candidate and it is
// never sudo-wrapped — the escalation only exists to reach root, and a root agent
// is already there (sudo may not even be installed).
func TestPowerCommandCandidatesRoot(t *testing.T) {
	want := map[string]map[proto.PowerAction][]string{
		"darwin": {
			proto.PowerSleep:    {"pmset", "sleepnow"},
			proto.PowerReboot:   {"shutdown", "-r", "now"},
			proto.PowerShutdown: {"shutdown", "-h", "now"},
		},
		"linux": {
			proto.PowerSleep:    {"systemctl", "suspend"},
			proto.PowerReboot:   {"systemctl", "reboot"},
			proto.PowerShutdown: {"systemctl", "poweroff"},
		},
		"windows": {
			proto.PowerSleep:    {"rundll32.exe", "powrprof.dll,SetSuspendState", "0,1,0"},
			proto.PowerReboot:   {"shutdown.exe", "/r", "/t", "0"},
			proto.PowerShutdown: {"shutdown.exe", "/s", "/t", "0"},
		},
	}
	for goos, table := range want {
		for action, wantArgv := range table {
			cands, err := powerCommandCandidates(goos, action, true)
			if err != nil {
				t.Fatalf("power %q on %s: unexpected error %v", action, goos, err)
			}
			if len(cands) != 1 {
				t.Fatalf("power %q on %s (root): want 1 candidate, got %v", action, goos, argvs(cands))
			}
			if cands[0].isSudo() {
				t.Errorf("power %q on %s (root): sudo-wrapped %v", action, goos, argv(cands[0]))
			}
			if got := argv(cands[0]); !reflect.DeepEqual(got, wantArgv) {
				t.Errorf("power %q on %s (root): got %v want %v", action, goos, got, wantArgv)
			}
		}
	}
}

// TestPowerCommandNeverPlainSudo: no candidate may ever be a bare `sudo` without
// -n. Plain sudo can block forever on a password prompt no daemon can answer —
// the objection D39 raised and D40 answers with the non-interactive flag.
func TestPowerCommandNeverPlainSudo(t *testing.T) {
	for _, goos := range []string{"darwin", "linux", "windows"} {
		for _, action := range []proto.PowerAction{proto.PowerSleep, proto.PowerReboot, proto.PowerShutdown} {
			for _, isRoot := range []bool{false, true} {
				cands, err := powerCommandCandidates(goos, action, isRoot)
				if err != nil {
					t.Fatalf("power %q on %s: %v", action, goos, err)
				}
				for _, c := range cands {
					if !c.isSudo() {
						continue
					}
					if len(c.args) == 0 || c.args[0] != "-n" {
						t.Errorf("power %q on %s (root=%v): sudo without -n: %v", action, goos, isRoot, argv(c))
					}
				}
			}
		}
	}
}

// TestPowerCommandUnknownAction: a bogus action errors instead of running
// something, on every OS.
func TestPowerCommandUnknownAction(t *testing.T) {
	for _, goos := range []string{"darwin", "linux", "windows", "plan9"} {
		for _, action := range []proto.PowerAction{"hibernate", "restart", "", "SHUTDOWN"} {
			if _, err := powerCommandCandidates(goos, action, false); err == nil {
				t.Errorf("expected error for unknown power action %q on %s", action, goos)
			}
		}
	}
}

// TestPowerCommandUnsupportedOS: an OS with no power mapping errors rather than
// silently returning nothing to run.
func TestPowerCommandUnsupportedOS(t *testing.T) {
	if _, err := powerCommandCandidates("freebsd", proto.PowerReboot, false); err == nil {
		t.Error("expected error for power on freebsd")
	}
}

// TestPowerPreflight covers the gate that makes the result truthful: only the
// provably-hopeless case (macOS, non-root, no NOPASSWD sudo) is refused, and it is
// refused with a message that names the user and the fix.
func TestPowerPreflight(t *testing.T) {
	cases := []struct {
		name       string
		goos       string
		action     proto.PowerAction
		isRoot     bool
		sudoOK     bool
		wantRefuse bool
	}{
		{"darwin reboot, no root, no sudo — the v0.2.2 bug", "darwin", proto.PowerReboot, false, false, true},
		{"darwin shutdown, no root, no sudo", "darwin", proto.PowerShutdown, false, false, true},
		{"darwin reboot, NOPASSWD sudo", "darwin", proto.PowerReboot, false, true, false},
		{"darwin reboot as root", "darwin", proto.PowerReboot, true, false, false},
		{"darwin sleep needs no privilege", "darwin", proto.PowerSleep, false, false, false},
		{"linux reboot may go through polkit", "linux", proto.PowerReboot, false, false, false},
		{"linux shutdown may go through polkit", "linux", proto.PowerShutdown, false, false, false},
		{"windows attempts and lets shutdown.exe answer", "windows", proto.PowerReboot, false, false, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			msg := powerPreflight(tc.goos, tc.action, tc.isRoot, tc.sudoOK, "emulationstation")
			if tc.wantRefuse && msg == "" {
				t.Fatalf("expected a refusal reason, got none")
			}
			if !tc.wantRefuse && msg != "" {
				t.Fatalf("expected no refusal, got %q", msg)
			}
			if !tc.wantRefuse {
				return
			}
			for _, want := range []string{string(tc.action), "emulationstation", "NOPASSWD", "Nothing was executed"} {
				if !strings.Contains(msg, want) {
					t.Errorf("refusal %q does not mention %q", msg, want)
				}
			}
		})
	}
}

// TestPowerPreflightNamesAUserWithoutOne: an empty username still produces a
// readable message rather than "user  has no...".
func TestPowerPreflightNamesAUserWithoutOne(t *testing.T) {
	msg := powerPreflight("darwin", proto.PowerReboot, false, false, "")
	if msg == "" || strings.Contains(msg, "user  ") {
		t.Fatalf("bad refusal for an unknown username: %q", msg)
	}
}

// TestNeedsSudoProbe: the `sudo -n true` probe is spent only where its answer can
// change the decision — an unprivileged unix halt/restart.
func TestNeedsSudoProbe(t *testing.T) {
	cases := []struct {
		goos   string
		action proto.PowerAction
		isRoot bool
		want   bool
	}{
		{"darwin", proto.PowerReboot, false, true},
		{"darwin", proto.PowerShutdown, false, true},
		{"linux", proto.PowerReboot, false, true},
		{"darwin", proto.PowerSleep, false, false},
		{"darwin", proto.PowerReboot, true, false},
		{"windows", proto.PowerReboot, false, false},
	}
	for _, tc := range cases {
		if got := needsSudoProbe(tc.goos, tc.action, tc.isRoot); got != tc.want {
			t.Errorf("needsSudoProbe(%s,%s,root=%v) = %v want %v", tc.goos, tc.action, tc.isRoot, got, tc.want)
		}
	}
}

// TestSudoRefused separates "sudo would not let me" (retry the bare command) from
// "the command itself failed" (do not retry — a second reboot attempt is worse
// than none).
func TestSudoRefused(t *testing.T) {
	refusals := []string{
		"sudo: a password is required",
		"sudo: a terminal is required to read the password; either use the -S option to read from standard input or configure an askpass helper",
		"sudo: no tty present and no askpass program specified",
		"emulationstation is not in the sudoers file.  This incident will be reported.",
		"Sorry, user emulationstation may not run /sbin/shutdown on emu.",
	}
	for _, out := range refusals {
		if !sudoRefused(out) {
			t.Errorf("expected a sudo refusal for %q", out)
		}
	}
	notRefusals := []string{
		"",
		"shutdown: NOT super-user",
		"Failed to reboot system via logind: Interactive authentication required.",
		"shutdown: Unable to shutdown system",
	}
	for _, out := range notRefusals {
		if sudoRefused(out) {
			t.Errorf("did not expect a sudo refusal for %q", out)
		}
	}
}

// TestRunPowerCandidatesFirstSuccessWins: once a candidate exits 0, nothing else
// runs — so a successful `sudo -n reboot` is never chased by a second bare reboot.
func TestRunPowerCandidatesFirstSuccessWins(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses /usr/bin/true and /usr/bin/false")
	}
	yes, err := exec.LookPath("true")
	if err != nil {
		t.Skipf("no true(1): %v", err)
	}
	no, err := exec.LookPath("false")
	if err != nil {
		t.Skipf("no false(1): %v", err)
	}
	// A failing second candidate would surface in the error if it ran.
	if err := runPowerCandidates(context.Background(), []powerCmd{{yes, nil}, {no, nil}}); err != nil {
		t.Fatalf("expected success from the first candidate, got %v", err)
	}
}

// TestRunPowerCandidatesReportsEveryFailure: when nothing works the error names
// each attempt, so the operator sees the real reason instead of v0.2.2's silence.
func TestRunPowerCandidatesReportsEveryFailure(t *testing.T) {
	err := runPowerCandidates(context.Background(), []powerCmd{
		{"sudo", []string{"-n", "lattice-no-such-command-xyz"}},
		{"lattice-no-such-command-xyz", nil},
	})
	if err == nil {
		t.Fatal("expected an error when no candidate can run")
	}
	if !strings.Contains(err.Error(), "lattice-no-such-command-xyz") {
		t.Errorf("error does not name the attempted command: %v", err)
	}
}

// TestRunPowerCandidatesEmpty: an empty candidate list is an error, not a silent
// no-op reported as success.
func TestRunPowerCandidatesEmpty(t *testing.T) {
	if err := runPowerCandidates(context.Background(), nil); err == nil {
		t.Fatal("expected an error for an empty candidate list")
	}
}

// TestPowerControlRefusesWithoutExecuting is the regression test for the live bug:
// a non-root macOS agent with no NOPASSWD sudo must answer ok=false and run
// NOTHING, instead of v0.2.2's ok=true over a machine that never rebooted.
func TestPowerControlRefusesWithoutExecuting(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("the hopeless case is macOS-specific")
	}
	if os.Geteuid() == 0 {
		t.Skip("test must run unprivileged")
	}
	orig := sudoNonInteractiveOK
	sudoNonInteractiveOK = func(context.Context) bool { return false }
	defer func() { sudoNonInteractiveOK = orig }()

	outbound := make(chan []byte, 4)
	powerControl(context.Background(), proto.PowerControlPayload{
		ReqID: "req-1", Action: proto.PowerReboot,
	}, outbound)

	select {
	case frame := <-outbound:
		env, err := proto.Decode(frame)
		if err != nil {
			t.Fatalf("decode: %v", err)
		}
		var res proto.PowerControlResultPayload
		if err := proto.As(env, &res); err != nil {
			t.Fatalf("as: %v", err)
		}
		if res.OK {
			t.Fatal("agent acked ok=true for a reboot it cannot perform (the v0.2.2 bug)")
		}
		if !strings.Contains(res.Error, "needs root") {
			t.Errorf("error does not explain the privilege problem: %q", res.Error)
		}
		if res.ReqID != "req-1" {
			t.Errorf("reqID not correlated: %q", res.ReqID)
		}
	default:
		t.Fatal("no result frame")
	}
}

// TestPowerControlRejectsUnknownAction: an invalid action answers ok=false and
// never reaches a command.
func TestPowerControlRejectsUnknownAction(t *testing.T) {
	outbound := make(chan []byte, 4)
	powerControl(context.Background(), proto.PowerControlPayload{
		ReqID: "req-2", Action: proto.PowerAction("hibernate"),
	}, outbound)

	frame := <-outbound
	env, err := proto.Decode(frame)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	var res proto.PowerControlResultPayload
	if err := proto.As(env, &res); err != nil {
		t.Fatalf("as: %v", err)
	}
	if res.OK || !strings.Contains(res.Error, "unknown power action") {
		t.Fatalf("want ok=false with an unknown-action error, got ok=%v err=%q", res.OK, res.Error)
	}
}

// TestSudoProbeCannotHang: the probe is `sudo -n` — non-interactive by
// construction — and is additionally bounded by a context. Asserting the shape
// here keeps a future edit from reintroducing a prompting sudo (D40).
func TestSudoProbeCannotHang(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // already dead: the probe must return, not block
	done := make(chan bool, 1)
	go func() { done <- sudoNonInteractiveOK(ctx) }()
	select {
	case ok := <-done:
		if ok {
			t.Error("a cancelled probe reported success")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("sudo probe hung on a cancelled context")
	}
}

// TestErrNotFoundFallsBack: a host with no sudo at all still tries the bare
// command rather than stopping at the missing wrapper.
func TestErrNotFoundFallsBack(t *testing.T) {
	err := runPowerCandidates(context.Background(), []powerCmd{
		{"lattice-no-such-sudo-xyz", []string{"-n", "true"}},
		{"lattice-also-missing-xyz", nil},
	})
	if err == nil {
		t.Fatal("expected an error")
	}
	if !strings.Contains(err.Error(), "lattice-also-missing-xyz") {
		t.Errorf("did not fall through to the second candidate: %v", err)
	}
}

// TestLANIPv4CIDRsArePrivate: every CIDR this host reports is a parseable,
// private-range IPv4 network in CIDR form — never a public/link-local address a
// WoL relay match would be wrong to use.
func TestLANIPv4CIDRsArePrivate(t *testing.T) {
	for _, c := range lanIPv4CIDRs() {
		ip, _, err := net.ParseCIDR(c)
		if err != nil {
			t.Fatalf("CIDR %q does not parse: %v", c, err)
		}
		if ip.To4() == nil {
			t.Fatalf("CIDR %q is not IPv4", c)
		}
		if !ip.IsPrivate() {
			t.Fatalf("CIDR %q is not a private address", c)
		}
	}
}
