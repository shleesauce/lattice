package agent

import (
	"testing"
	"time"
)

// idleThreshold honors LATTICE_IDLE_SECS but rejects junk / too-small values back
// to the 45s default — a bad env var can never arm a hair-trigger (or never-fire)
// idle notification.
func TestIdleThreshold(t *testing.T) {
	const def = 45 * time.Second

	cases := []struct {
		env  string
		want time.Duration
	}{
		{"", def},
		{"  ", def},
		{"garbage", def},
		{"0", def}, // below the 5s floor
		{"3", def}, // below the 5s floor
		{"5", 5 * time.Second},
		{"90", 90 * time.Second},
		{" 120 ", 120 * time.Second},
		{"-10", def},
	}
	for _, c := range cases {
		t.Setenv("LATTICE_IDLE_SECS", c.env)
		if got := idleThreshold(); got != c.want {
			t.Errorf("idleThreshold() with LATTICE_IDLE_SECS=%q = %v, want %v", c.env, got, c.want)
		}
	}
}
