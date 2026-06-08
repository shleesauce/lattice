package agent

import (
	"os"
	"path/filepath"
	"testing"
)

// resolveListPath must re-root a synced under-home path (".../<home>/<rest>")
// onto THIS machine's home so the dock file browser works for a session placed on
// a remote agent whose home differs from the hub's (D23 / F17). It must also map
// empty → home and leave unrelated absolute paths untouched.
func TestResolveListPathRebasesProjectPaths(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		t.Skip("no home dir on this machine")
	}

	cases := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "remote hub path rebased to local home",
			in:   "/Users/some-other-user/projects/lattice-qa-probe",
			want: filepath.Join(home, "projects/lattice-qa-probe"),
		},
		{
			name: "nested subdir preserved on rebase",
			in:   "/Users/some-other-user/projects/lattice/docs",
			want: filepath.Join(home, "projects/lattice/docs"),
		},
		{
			name: "already-local path is idempotent",
			in:   filepath.Join(home, "projects/lattice-qa-probe"),
			want: filepath.Join(home, "projects/lattice-qa-probe"),
		},
		{
			name: "empty resolves to home",
			in:   "",
			want: home,
		},
		{
			name: "unrelated absolute path untouched",
			in:   "/etc",
			want: "/etc",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := resolveListPath(tc.in)
			if err != nil {
				t.Fatalf("resolveListPath(%q) error: %v", tc.in, err)
			}
			if got != tc.want {
				t.Errorf("resolveListPath(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}
