package agent

import (
	"net"
	"reflect"
	"runtime"
	"testing"

	"github.com/shleesauce/lattice/internal/proto"
)

// TestPowerCommandKnownActions: sleep, reboot + shutdown all resolve to a
// concrete command on the host OS this test runs on (darwin/linux/windows).
func TestPowerCommandKnownActions(t *testing.T) {
	for _, action := range []proto.PowerAction{proto.PowerSleep, proto.PowerReboot, proto.PowerShutdown} {
		cmd, err := powerCommand(action)
		if err != nil {
			t.Fatalf("power %q on %s: unexpected error %v", action, runtime.GOOS, err)
		}
		if cmd.name == "" {
			t.Fatalf("power %q on %s: empty command", action, runtime.GOOS)
		}
	}
}

// TestPowerCommandPerOS pins the exact argv each action maps to on THIS build's
// GOOS — the part a careless edit would silently change (e.g. shutdown -r →
// shutdown -h, halting a box the operator asked to restart). Table-driven and
// pure: powerCommand only constructs the command, it never runs one.
func TestPowerCommandPerOS(t *testing.T) {
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
	table, ok := want[runtime.GOOS]
	if !ok {
		t.Skipf("no expected power argv for %s", runtime.GOOS)
	}
	for action, argv := range table {
		cmd, err := powerCommand(action)
		if err != nil {
			t.Fatalf("power %q on %s: unexpected error %v", action, runtime.GOOS, err)
		}
		got := append([]string{cmd.name}, cmd.args...)
		if !reflect.DeepEqual(got, argv) {
			t.Errorf("power %q on %s: got %v want %v", action, runtime.GOOS, got, argv)
		}
	}
}

// TestPowerCommandUnknownAction: a bogus action errors instead of running
// something.
func TestPowerCommandUnknownAction(t *testing.T) {
	for _, action := range []proto.PowerAction{"hibernate", "restart", "", "SHUTDOWN"} {
		if _, err := powerCommand(action); err == nil {
			t.Errorf("expected error for unknown power action %q", action)
		}
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
