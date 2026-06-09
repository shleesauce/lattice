package hub

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

// releasesAPI is the GitHub Releases endpoint for this repo. Read-only, public, no
// token — the unauthenticated rate limit (60/h per IP) is plenty behind the cache.
const releasesAPI = "https://api.github.com/repos/shleesauce/lattice/releases"

// releaseCacheTTL bounds how often the hub hits GitHub. Release notes + the update
// check don't need to be fresher than this, and it keeps us well under the limit.
const releaseCacheTTL = 30 * time.Minute

// releaseInfo is the trimmed release shape the dashboard consumes.
type releaseInfo struct {
	Version     string `json:"version"` // tag, e.g. "v0.1.5"
	Name        string `json:"name"`
	Body        string `json:"body"` // markdown release notes
	PublishedAt string `json:"publishedAt"`
	Prerelease  bool   `json:"prerelease"`
	URL         string `json:"url"`
}

// ghRelease is the subset of the GitHub Releases API JSON we read.
type ghRelease struct {
	TagName     string `json:"tag_name"`
	Name        string `json:"name"`
	Body        string `json:"body"`
	PublishedAt string `json:"published_at"`
	Prerelease  bool   `json:"prerelease"`
	Draft       bool   `json:"draft"`
	HTMLURL     string `json:"html_url"`
}

// releaseCache memoizes the GitHub release list with a TTL, and serves the last
// good copy if a later refresh fails (stale-while-error) so a GitHub blip doesn't
// blank the release-notes panel or flap the update banner.
type releaseCache struct {
	mu        sync.Mutex
	releases  []releaseInfo
	fetchedAt time.Time
}

func newReleaseCache() *releaseCache { return &releaseCache{} }

// fetchReleases returns the recent releases, refreshing from GitHub past the TTL.
// On a fetch error it falls back to the cached copy (possibly empty) rather than
// failing — release notes are informational, never load-bearing.
func (h *Hub) fetchReleases(ctx context.Context) ([]releaseInfo, error) {
	c := h.releases
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.releases != nil && time.Since(c.fetchedAt) < releaseCacheTTL {
		return c.releases, nil
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, releasesAPI+"?per_page=20", nil)
	if err != nil {
		return c.releases, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	resp, err := (&http.Client{Timeout: 10 * time.Second}).Do(req)
	if err != nil {
		return c.releases, err // serve stale on network error
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return c.releases, nil // rate-limited / unavailable: keep stale, no hard error
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return c.releases, err
	}
	var gh []ghRelease
	if err := json.Unmarshal(body, &gh); err != nil {
		return c.releases, err
	}

	out := make([]releaseInfo, 0, len(gh))
	for _, r := range gh {
		if r.Draft {
			continue
		}
		name := r.Name
		if name == "" {
			name = r.TagName
		}
		out = append(out, releaseInfo{
			Version:     r.TagName,
			Name:        name,
			Body:        r.Body,
			PublishedAt: r.PublishedAt,
			Prerelease:  r.Prerelease,
			URL:         r.HTMLURL,
		})
	}
	c.releases = out
	c.fetchedAt = time.Now()
	return out, nil
}

// latestStable returns the newest non-prerelease release, or false if none.
func latestStable(releases []releaseInfo) (releaseInfo, bool) {
	for _, r := range releases {
		if !r.Prerelease {
			return r, true
		}
	}
	return releaseInfo{}, false
}

// handleReleases serves the release-notes panel (admin-gated): the recent releases
// with the running build flagged, plus the resolved latest-stable + whether an
// update is available. Reused by the update banner (H).
func (h *Hub) handleReleases(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", "GET")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	releases, _ := h.fetchReleases(r.Context()) // stale/empty on error — never fail the panel

	type releaseView struct {
		releaseInfo
		Current bool `json:"current"` // == the running build
		Newer   bool `json:"newer"`   // strictly newer than the running build
	}
	views := make([]releaseView, 0, len(releases))
	for _, rel := range releases {
		views = append(views, releaseView{
			releaseInfo: rel,
			Current:     sameVersion(rel.Version, h.version),
			Newer:       versionNewer(rel.Version, h.version),
		})
	}

	latest := ""
	updateAvailable := false
	if ls, ok := latestStable(releases); ok {
		latest = ls.Version
		updateAvailable = versionNewer(ls.Version, h.version)
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"current":         h.version,
		"latest":          latest,
		"updateAvailable": updateAvailable,
		"releases":        views,
	})
}

// --- semver helpers (lenient: tolerate a leading "v", any "-…"/"+…" suffix, and
// non-numeric junk, which parses to 0). ---

// parseVersion turns "v0.1.5-3-gabc123" into {0,1,5}. The suffix is intentionally
// DROPPED, not weighted: it can mean either a SemVer prerelease ("v0.1.5-rc1",
// before the tag) OR git-describe's commits-after-tag ("v0.1.4-5-gabc", after the
// tag) — same syntax, opposite meaning. Comparing on the clean x.y.z is correct
// for real installs (always a clean release tag) and avoids ever telling a local
// dev build it's "behind" the release it was built on top of.
func parseVersion(s string) [3]int {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "v")
	if i := strings.IndexAny(s, "-+"); i >= 0 {
		s = s[:i]
	}
	var nums [3]int
	parts := strings.Split(s, ".")
	for i := 0; i < 3 && i < len(parts); i++ {
		nums[i], _ = strconv.Atoi(parts[i])
	}
	return nums
}

// versionNewer reports whether release a is strictly newer than build b by x.y.z.
func versionNewer(a, b string) bool {
	an, bn := parseVersion(a), parseVersion(b)
	for i := 0; i < 3; i++ {
		if an[i] != bn[i] {
			return an[i] > bn[i]
		}
	}
	return false
}

// sameVersion reports whether two version strings name the same release, ignoring
// a leading "v" (so the GitHub tag "v0.1.4" matches a build stamped "v0.1.4").
func sameVersion(a, b string) bool {
	return strings.TrimPrefix(strings.TrimSpace(a), "v") == strings.TrimPrefix(strings.TrimSpace(b), "v")
}
