package hub

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// registerProject best-effort registers a freshly scaffolded project into the
// configured project registry: it appends a row to the "## Project Registry"
// markdown table in the file at h.projectRegistryPath. The step is additive — any
// failure becomes a warning, never aborting the create. Returns the accumulated
// warnings. Callers gate this on the registry being enabled + a path being set.
func (h *Hub) registerProject(s projectSpec) []string {
	if w := appendRegistryRow(h.projectRegistryPath, s); w != "" {
		return []string{w}
	}
	return nil
}

// appendRegistryRow inserts a new row into the "## Project Registry" markdown
// table in the file at path. Missing file, or no table found, returns a warning.
func appendRegistryRow(path string, s projectSpec) string {
	row := registryRow(s)
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Sprintf("registry: read %s failed: %v", filepath.Base(path), err)
	}
	updated, ok := insertRegistryRow(string(data), row)
	if !ok {
		return "registry: no '## Project Registry' table found in " +
			filepath.Base(path) + "; add the row manually"
	}
	if err := os.WriteFile(path, []byte(updated), 0o644); err != nil {
		return fmt.Sprintf("registry: write %s failed: %v", filepath.Base(path), err)
	}
	return ""
}

// registryRow builds the markdown row matching the existing columns:
// | Project | Purpose | Port | Stack | Supabase | Status |
// Every free-text field is run through mdCell so a stray '|' or newline in the
// description/stack can't break out of its cell — which would corrupt the table
// (or splice in extra rows).
func registryRow(s projectSpec) string {
	return fmt.Sprintf("| %s | %s | %s | %s | — | Active |",
		mdCell(s.folder), mdCell(s.desc), portOrDash(s.port), mdCell(dashIfEmpty(s.stack)))
}

// mdCell sanitizes a value for safe interpolation into a single markdown table
// cell: CR/LF collapse to a space (a newline would terminate the row, splicing
// the rest of the text in as fresh table rows), and a literal '|' is escaped as
// '\|' (an unescaped pipe opens a new cell / column). Surrounding space is then
// trimmed so the cell stays tidy.
func mdCell(s string) string {
	s = strings.ReplaceAll(s, "\r\n", " ")
	s = strings.ReplaceAll(s, "\r", " ")
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.ReplaceAll(s, "|", "\\|")
	return strings.TrimSpace(s)
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
