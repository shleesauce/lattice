package hub

import (
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/shleesauce/lattice/internal/proto"
)

// The dashboard's dashboard/src/types.ts hand-mirrors these Go wire structs.
// There is no compiler linking the two, so a field added/renamed on one side
// silently breaks the other at runtime. This test is that missing link: it
// reflects each Go struct's JSON field names and asserts they match the fields
// declared on the corresponding TypeScript interface. When it fails, it names
// exactly which fields drifted and on which side — update both, then it passes.
//
// It checks field NAMES (the JSON contract), not types — Go uint64/float64 both
// land as TS `number`, and optionality is a TS concern. Adding a new wire field
// means: add it to the Go struct AND to the TS interface below's mirror.
func TestWireContractMatchesDashboardTypes(t *testing.T) {
	cases := []struct {
		tsInterface string      // interface name in types.ts
		goStruct    interface{} // a zero value of the Go wire struct
	}{
		{"Capabilities", proto.Capabilities{}},
		{"Agent", Agent{}},
		{"Device", Device{}},
		{"FileEntry", proto.FileEntry{}},
		{"FileListResult", proto.FileListResultPayload{}},
		{"Session", sessionView{}},
	}

	tsFields := parseTSInterfaces(t)

	for _, c := range cases {
		t.Run(c.tsInterface, func(t *testing.T) {
			ts, ok := tsFields[c.tsInterface]
			if !ok {
				t.Fatalf("interface %q not found in types.ts — was it renamed or removed?", c.tsInterface)
			}
			goNames := goJSONFields(reflect.TypeOf(c.goStruct))

			missingInTS := difference(goNames, ts)
			missingInGo := difference(ts, goNames)
			if len(missingInTS) > 0 || len(missingInGo) > 0 {
				t.Errorf("wire contract drift between Go %T and TS interface %q:\n"+
					"  in Go but missing from types.ts: %v\n"+
					"  in types.ts but missing from Go: %v\n"+
					"  → add the field to both sides so the dashboard and hub agree.",
					c.goStruct, c.tsInterface, missingInTS, missingInGo)
			}
		})
	}
}

// goJSONFields returns the set of JSON field names a struct serializes to,
// honoring `json:"name,omitempty"` and skipping `json:"-"` and untagged fields.
func goJSONFields(t reflect.Type) map[string]bool {
	out := map[string]bool{}
	for i := 0; i < t.NumField(); i++ {
		tag := t.Field(i).Tag.Get("json")
		if tag == "" || tag == "-" {
			continue
		}
		name := strings.Split(tag, ",")[0]
		if name != "" {
			out[name] = true
		}
	}
	return out
}

var (
	tsInterfaceRe = regexp.MustCompile(`export interface (\w+)\s*\{`)
	tsFieldRe     = regexp.MustCompile(`^\s*([a-zA-Z_][a-zA-Z0-9_]*)\??\s*:`)
)

// parseTSInterfaces reads dashboard/src/types.ts and returns, per top-level
// `export interface`, the set of its field names (stripping the optional `?`).
// It tracks brace depth so only depth-1 fields are collected and ignores
// comments and index signatures.
func parseTSInterfaces(t *testing.T) map[string]map[string]bool {
	t.Helper()
	path := filepath.Join("..", "..", "dashboard", "src", "types.ts")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}

	out := map[string]map[string]bool{}
	var cur string
	depth := 0
	for _, line := range strings.Split(string(raw), "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "//") {
			continue
		}
		if cur == "" {
			if m := tsInterfaceRe.FindStringSubmatch(line); m != nil {
				cur = m[1]
				out[cur] = map[string]bool{}
				depth = strings.Count(line, "{") - strings.Count(line, "}")
				if depth <= 0 {
					cur = "" // single-line interface (none today) — skip
				}
			}
			continue
		}
		// Inside an interface: collect only depth-1 fields.
		if depth == 1 {
			if m := tsFieldRe.FindStringSubmatch(line); m != nil {
				out[cur][m[1]] = true
			}
		}
		depth += strings.Count(line, "{") - strings.Count(line, "}")
		if depth <= 0 {
			cur = ""
		}
	}
	return out
}

func difference(a, b map[string]bool) []string {
	var out []string
	for k := range a {
		if !b[k] {
			out = append(out, k)
		}
	}
	sort.Strings(out)
	return out
}
