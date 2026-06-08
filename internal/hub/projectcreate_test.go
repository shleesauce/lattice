package hub

import (
	"strings"
	"testing"
)

func TestFolderNameRe(t *testing.T) {
	// The spec regex ^[a-z0-9][a-z0-9-]*$ allows a trailing hyphen (the tail class
	// includes "-"); only the leading char is restricted to [a-z0-9].
	valid := []string{"a", "ab", "my-project", "x1", "lattice", "a-b-c", "0name", "trailing-"}
	for _, v := range valid {
		if !folderNameRe.MatchString(v) {
			t.Errorf("expected %q to be a valid folderName", v)
		}
	}
	invalid := []string{"", "-leading", "Upper", "has space", "under_score", "x/y", "café", "a--b!"}
	for _, v := range invalid {
		if folderNameRe.MatchString(v) {
			t.Errorf("expected %q to be an invalid folderName", v)
		}
	}
}

func TestInsertRegistryRow(t *testing.T) {
	content := strings.Join([]string{
		"# Heading",
		"",
		"## Project Registry",
		"",
		"| Project | Purpose | Port | Stack | Supabase | Status |",
		"|---------|---------|------|-------|----------|--------|",
		"| webapp | reads | 5100 | React | abc | Active |",
		"| api-service | pipeline | 5700 | Next.js | — | Active |",
		"",
		"### Port Map (quick reference)",
		"trailing content",
	}, "\n")

	row := "| newproj | a new thing | 5999 | Go | — | Active |"
	out, ok := insertRegistryRow(content, row)
	if !ok {
		t.Fatal("expected the registry table to be found")
	}

	lines := strings.Split(out, "\n")
	// Row must land immediately after the last existing data row (api-service).
	var rowIdx, lastIdx, blankAfter int
	for i, ln := range lines {
		switch {
		case ln == row:
			rowIdx = i
		case strings.HasPrefix(ln, "| api-service "):
			lastIdx = i
		case ln == "" && i > lastIdx && blankAfter == 0:
			blankAfter = i
		}
	}
	if rowIdx == 0 {
		t.Fatal("inserted row not found in output")
	}
	if rowIdx != lastIdx+1 {
		t.Fatalf("row inserted at %d, expected right after last data row %d", rowIdx, lastIdx)
	}
	// The trailing non-table content must be preserved.
	if !strings.Contains(out, "### Port Map (quick reference)") || !strings.Contains(out, "trailing content") {
		t.Fatal("trailing content was not preserved")
	}
	// Exactly one new line added.
	if got := strings.Count(out, "\n"); got != strings.Count(content, "\n")+1 {
		t.Fatalf("expected exactly one added line, got delta %d", got-strings.Count(content, "\n"))
	}
}

func TestInsertRegistryRowNoTable(t *testing.T) {
	content := "# Just a heading\n\nNo registry here.\n"
	if _, ok := insertRegistryRow(content, "| x | y | — | — | — | Active |"); ok {
		t.Fatal("expected no table to be found")
	}
}

func TestRegistryRowFormat(t *testing.T) {
	s := projectSpec{folder: "foo", desc: "does foo", stack: "Go", port: 8080}
	got := registryRow(s)
	want := "| foo | does foo | 8080 | Go | — | Active |"
	if got != want {
		t.Fatalf("registryRow = %q, want %q", got, want)
	}

	s2 := projectSpec{folder: "bar", desc: "no extras"}
	got2 := registryRow(s2)
	want2 := "| bar | no extras | — | — | — | Active |"
	if got2 != want2 {
		t.Fatalf("registryRow = %q, want %q", got2, want2)
	}
}
