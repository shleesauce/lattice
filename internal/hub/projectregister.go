package hub

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// registerProject best-effort registers a freshly scaffolded project into the
// AI-Hub: (a) appends a row to the Project Registry markdown table, (b) regenerates
// docs/PROJECT_INDEX.md, (c) writes a knowledge-base wiki stub. Each step is
// independent and additive — any failure becomes a warning, never aborting the
// create. Returns the accumulated warnings.
func (h *Hub) registerProject(aiHubRoot string, s projectSpec) []string {
	var warnings []string

	// (a) Registry table row. The canonical Project Registry table that the index
	// generator reads lives in UNIVERSAL_RULES.md; CLAUDE.md is the fallback. We
	// insert into whichever actually contains the "## Project Registry" table.
	if w := appendRegistryRow(aiHubRoot, s); w != "" {
		warnings = append(warnings, w)
	}

	// (b) Regenerate the project index from the registry table.
	if w := runIndexScript(aiHubRoot); w != "" {
		warnings = append(warnings, w)
	}

	// (c) Knowledge-base wiki stub.
	if w := writeWikiStub(aiHubRoot, s); w != "" {
		warnings = append(warnings, w)
	}

	return warnings
}

// registryFiles lists the candidate files that may hold the Project Registry
// table, in preference order: the canonical UNIVERSAL_RULES.md (what
// build-project-index.sh parses) first, then CLAUDE.md.
func registryFiles(aiHubRoot string) []string {
	return []string{
		filepath.Join(aiHubRoot, "UNIVERSAL_RULES.md"),
		filepath.Join(aiHubRoot, "CLAUDE.md"),
	}
}

// appendRegistryRow inserts a new row into the Project Registry markdown table.
func appendRegistryRow(aiHubRoot string, s projectSpec) string {
	row := registryRow(s)
	for _, path := range registryFiles(aiHubRoot) {
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		updated, ok := insertRegistryRow(string(data), row)
		if !ok {
			continue
		}
		if err := os.WriteFile(path, []byte(updated), 0o644); err != nil {
			return fmt.Sprintf("registry: write %s failed: %v", filepath.Base(path), err)
		}
		return ""
	}
	return "registry: no '## Project Registry' table found (CLAUDE.md/UNIVERSAL_RULES.md); add the row manually"
}

// registryRow builds the markdown row matching the existing columns:
// | Project | Purpose | Port | Stack | Supabase | Status |
func registryRow(s projectSpec) string {
	return fmt.Sprintf("| %s | %s | %s | %s | — | Active |",
		s.folder, s.desc, portOrDash(s.port), dashIfEmpty(s.stack))
}

// insertRegistryRow inserts row after the LAST consecutive table row that follows
// the "| Project " header and its "|---" separator. The file is preserved
// byte-for-byte otherwise. Returns the new content and whether the table was found.
func insertRegistryRow(content, row string) (string, bool) {
	lines := strings.Split(content, "\n")

	header := -1
	for i, ln := range lines {
		if strings.HasPrefix(ln, "| Project ") {
			header = i
			break
		}
	}
	if header == -1 {
		return content, false
	}
	// The next line must be the |--- separator.
	sep := header + 1
	if sep >= len(lines) || !strings.HasPrefix(strings.TrimSpace(lines[sep]), "|-") {
		return content, false
	}

	// Walk past every consecutive data row (a line whose trimmed form starts "|").
	last := sep
	for i := sep + 1; i < len(lines); i++ {
		if strings.HasPrefix(strings.TrimSpace(lines[i]), "|") {
			last = i
			continue
		}
		break
	}

	out := make([]string, 0, len(lines)+1)
	out = append(out, lines[:last+1]...)
	out = append(out, row)
	out = append(out, lines[last+1:]...)
	return strings.Join(out, "\n"), true
}

// runIndexScript regenerates docs/PROJECT_INDEX.md via the AI-Hub's own script.
func runIndexScript(aiHubRoot string) string {
	script := filepath.Join(aiHubRoot, "scripts", "build-project-index.sh")
	if _, err := os.Stat(script); err != nil {
		return "index: scripts/build-project-index.sh not found; PROJECT_INDEX.md not regenerated"
	}
	ctx, cancel := context.WithTimeout(context.Background(), indexScriptTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "bash", script)
	cmd.Dir = aiHubRoot
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Sprintf("index: build-project-index.sh failed: %v: %s", err, strings.TrimSpace(string(out)))
	}
	return ""
}

// writeWikiStub writes a short knowledge-base article for the project IF the
// wiki/projects directory exists. _index.md / _map.md are left untouched (they
// need manual linking) and that is surfaced as a warning.
func writeWikiStub(aiHubRoot string, s projectSpec) string {
	wikiDir := filepath.Join(aiHubRoot, "knowledge-base", "wiki", "projects")
	if info, err := os.Stat(wikiDir); err != nil || !info.IsDir() {
		return "wiki: knowledge-base/wiki/projects not found; skipped the project stub"
	}
	path := filepath.Join(wikiDir, s.folder+".md")
	if _, err := os.Stat(path); err == nil {
		return "wiki: " + s.folder + ".md already exists; left untouched"
	}
	if err := os.WriteFile(path, []byte(wikiStub(s)), 0o644); err != nil {
		return "wiki: write stub failed: " + err.Error()
	}
	return "wiki: stub written; link it manually in wiki/projects/_index.md and _map.md"
}

func wikiStub(s projectSpec) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# %s\n\n", s.official)
	fmt.Fprintf(&b, "%s\n\n", s.desc)
	b.WriteString("- Status: new\n")
	fmt.Fprintf(&b, "- Created: %s\n", s.created.Format(time.RFC3339))
	if s.stack != "" {
		fmt.Fprintf(&b, "- Stack: %s\n", s.stack)
	}
	if len(s.related) > 0 {
		fmt.Fprintf(&b, "- Related: %s\n", strings.Join(s.related, ", "))
	}
	b.WriteString("\nScaffolded via Lattice.\n")
	return b.String()
}
