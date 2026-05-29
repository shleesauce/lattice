package hub

import (
	"embed"
	"io/fs"
)

// distFS embeds the built dashboard. The build script copies dashboard/dist
// here before `go build`, so the shipped hub binary serves the UI with zero
// external files (packageability: one artifact). The placeholder index.html
// keeps this compiling before the dashboard is built.
//
//go:embed all:web/dist
var distFS embed.FS

// DashboardFS returns the embedded dashboard rooted at web/dist.
func DashboardFS() (fs.FS, error) {
	return fs.Sub(distFS, "web/dist")
}
