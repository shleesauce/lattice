package proto

import "testing"

// TestPowerActionValid pins the accepted power-action set. Both the hub's request
// validation and the agent's frame validation gate on PowerAction.Valid, so this
// table IS the contract — widening it here widens what any machine on the fleet
// will do to itself.
func TestPowerActionValid(t *testing.T) {
	cases := []struct {
		action PowerAction
		want   bool
	}{
		{PowerSleep, true},
		{PowerReboot, true},
		{PowerShutdown, true},
		{"", false},
		{"hibernate", false},
		{"restart", false},
		{"Reboot", false},  // case-sensitive: the wire value is lowercase
		{"reboot ", false}, // the hub trims before validating; Valid itself does not
	}
	for _, c := range cases {
		if got := c.action.Valid(); got != c.want {
			t.Errorf("PowerAction(%q).Valid() = %v, want %v", c.action, got, c.want)
		}
	}
}

// TestPowerControlRoundTrip: a reboot request and its result survive
// Encode → Decode → As unchanged, so the new action can't drift on a struct-tag
// or constant edit.
func TestPowerControlRoundTrip(t *testing.T) {
	in := PowerControlPayload{ReqID: "r1", Action: PowerReboot}
	var out PowerControlPayload
	roundTrip(t, TypePowerControl, in, &out)
	if in != out {
		t.Fatalf("request: got %+v want %+v", out, in)
	}

	inRes := PowerControlResultPayload{ReqID: "r1", Action: string(PowerReboot), OK: true}
	var outRes PowerControlResultPayload
	roundTrip(t, TypePowerControlResult, inRes, &outRes)
	if inRes != outRes {
		t.Fatalf("result: got %+v want %+v", outRes, inRes)
	}
}
