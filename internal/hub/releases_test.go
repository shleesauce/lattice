package hub

import "testing"

func TestVersionNewer(t *testing.T) {
	cases := []struct {
		a, b string
		want bool
	}{
		{"v0.1.5", "v0.1.4", true},
		{"v0.1.4", "v0.1.5", false},
		{"v0.1.4", "v0.1.4", false},
		{"0.2.0", "v0.1.9", true},
		{"v1.0.0", "v0.9.9", true},
		{"v0.1.5", "v0.1.5-2-gabc123", false}, // same x.y.z (suffix dropped) → not newer
		{"v0.1.5-2-gabc123", "v0.1.5", false}, // same x.y.z → not newer
		{"v0.1.4", "dev", true},               // an unstamped "dev" build parses to {0,0,0}, so 0.1.4 is newer
		{"v0.1.5", "v0.1.5", false},           // identical
	}
	for _, c := range cases {
		if got := versionNewer(c.a, c.b); got != c.want {
			t.Errorf("versionNewer(%q, %q) = %v, want %v", c.a, c.b, got, c.want)
		}
	}
}

func TestVersionNewerAgainstDevBuild(t *testing.T) {
	// A locally-built hub stamped "v0.1.4-5-g8951899-dirty" must NOT report v0.1.4
	// as an available update (local is ahead of the last release).
	if versionNewer("v0.1.4", "v0.1.4-5-g8951899-dirty") {
		t.Error("a published release should not look newer than a dev build ahead of it")
	}
}

func TestSameVersion(t *testing.T) {
	if !sameVersion("v0.1.4", "0.1.4") {
		t.Error("sameVersion should ignore a leading v")
	}
	if sameVersion("v0.1.4", "v0.1.5") {
		t.Error("different versions must not match")
	}
}

func TestLatestStable(t *testing.T) {
	releases := []releaseInfo{
		{Version: "v0.2.0-rc1", Prerelease: true},
		{Version: "v0.1.5", Prerelease: false},
		{Version: "v0.1.4", Prerelease: false},
	}
	got, ok := latestStable(releases)
	if !ok || got.Version != "v0.1.5" {
		t.Errorf("latestStable = %q (ok=%v), want v0.1.5", got.Version, ok)
	}
	if _, ok := latestStable([]releaseInfo{{Version: "v1-rc", Prerelease: true}}); ok {
		t.Error("latestStable should report none when all are prereleases")
	}
}
