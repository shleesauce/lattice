package hub

import "testing"

func TestIsPubliclyBound(t *testing.T) {
	cases := []struct {
		addr string
		want bool
	}{
		// Wildcard binds = all interfaces = public.
		{":7400", true},
		{"0.0.0.0:7400", true},
		{"[::]:7400", true},
		// Loopback = private.
		{"127.0.0.1:7400", false},
		{"localhost:7400", false},
		{"[::1]:7400", false},
		// A specific routable IP or hostname = public.
		{"100.64.0.1:7400", true},
		{"host.example.ts.net:7400", true},
		// No port still classifies by host.
		{"127.0.0.1", false},
		{"0.0.0.0", true},
	}
	for _, c := range cases {
		if got := isPubliclyBound(c.addr); got != c.want {
			t.Errorf("isPubliclyBound(%q) = %v, want %v", c.addr, got, c.want)
		}
	}
}
