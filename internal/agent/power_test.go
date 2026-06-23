package agent

import (
	"net"
	"runtime"
	"testing"

	"github.com/shleesauce/lattice/internal/proto"
)

// TestPowerCommandKnownActions: sleep + shutdown both resolve to a concrete
// command on the host OS this test runs on (darwin/linux/windows).
func TestPowerCommandKnownActions(t *testing.T) {
	for _, action := range []proto.PowerAction{proto.PowerSleep, proto.PowerShutdown} {
		cmd, err := powerCommand(action)
		if err != nil {
			t.Fatalf("power %q on %s: unexpected error %v", action, runtime.GOOS, err)
		}
		if cmd.name == "" {
			t.Fatalf("power %q on %s: empty command", action, runtime.GOOS)
		}
	}
}

// TestPowerCommandUnknownAction: a bogus action errors instead of running
// something.
func TestPowerCommandUnknownAction(t *testing.T) {
	if _, err := powerCommand(proto.PowerAction("reboot")); err == nil {
		t.Fatalf("expected error for unknown power action")
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
