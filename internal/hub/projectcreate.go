package hub

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/shleesauce/lattice/internal/proto"
)

// folderNameRe constrains a project folder to a lowercase, hyphen-safe slug so
// it is a valid directory name AND a clean project-registry key.
var folderNameRe = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)

// gitTimeout bounds the best-effort git init/commit so a slow or hung git can't
// stall the create request.
const gitTimeout = 15 * time.Second

// envVar is one scaffolded environment variable.
type envVar struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

// createProjectBody is the POST /api/projects request. The UI supplies the
// folderName verbatim (nothing is derived server-side). register/launchClaude
// default to true when omitted, so they are pointers.
type createProjectBody struct {
	OfficialName    string   `json:"officialName"`
	FolderName      string   `json:"folderName"`
	Description     string   `json:"description"`
	Stack           string   `json:"stack"`
	Port            int      `json:"port"`
	Connectors      []string `json:"connectors"`
	Agents          []string `json:"agents"`
	RelatedProjects []string `json:"relatedProjects"`
	EnvVars         []envVar `json:"envVars"`
	Register        *bool    `json:"register"`
	LaunchClaude    *bool    `json:"launchClaude"`
}

// handleCreateProject scaffolds a brand-new project on the hub host, optionally
// registers it into the configured project registry, and optionally auto-launches
// a Claude session seeded with the onboarding brief to finish the AI-specific
// wiring.
//
// The scaffold + brief are written deterministically by the hub (so they exist
// the moment the call returns and propagate to every machine via Syncthing); the
// Claude session reads docs/ONBOARDING.md and completes the connectors/MCPs/
// agents/env wiring that the hub deliberately does NOT assemble.
func (h *Hub) handleCreateProject(w http.ResponseWriter, r *http.Request) {
	var body createProjectBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid json body"})
		return
	}

	official := strings.TrimSpace(body.OfficialName)
	folder := strings.TrimSpace(body.FolderName)
	desc := strings.TrimSpace(body.Description)
	stack := strings.TrimSpace(body.Stack)

	if official == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "officialName is required"})
		return
	}
	if !folderNameRe.MatchString(folder) {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error": "folderName must match ^[a-z0-9][a-z0-9-]*$",
		})
		return
	}
	if desc == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "description is required"})
		return
	}

	root := h.ProjectsRoot()
	projDir := filepath.Join(root, folder)

	if _, err := os.Stat(projDir); err == nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error": "a project named " + folder + " already exists",
		})
		return
	} else if !os.IsNotExist(err) {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}

	// Registration appends a row to the configured project-registry markdown file.
	// It only runs when the hub has the registry enabled AND a registry path set;
	// a stock hub skips it regardless of the request flag.
	registryEnabled := h.projectRegistry && h.projectRegistryPath != ""
	register := (body.Register == nil || *body.Register) && registryEnabled
	launchClaude := body.LaunchClaude == nil || *body.LaunchClaude

	now := time.Now()
	spec := projectSpec{
		official:   official,
		folder:     folder,
		desc:       desc,
		stack:      stack,
		port:       body.Port,
		connectors: cleanList(body.Connectors),
		agents:     cleanList(body.Agents),
		related:    cleanList(body.RelatedProjects),
		envVars:    cleanEnv(body.EnvVars),
		created:    now,
	}

	var warnings []string

	if err := scaffoldProject(projDir, spec); err != nil {
		// Scaffold is the core deliverable: if it fails, the create fails. Best-effort
		// remove a partial tree so a retry with the same name isn't blocked.
		_ = os.RemoveAll(projDir)
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "scaffold failed: " + err.Error()})
		return
	}

	if w := gitInitProject(projDir); w != "" {
		warnings = append(warnings, w)
	}

	if register {
		warnings = append(warnings, h.registerProject(spec)...)
	} else if (body.Register == nil || *body.Register) && !registryEnabled {
		warnings = append(warnings, "registration skipped: project registry is disabled on this hub")
	}

	var sessionV *sessionView
	if launchClaude {
		view, launchWarns := h.launchOnboardingSession(projDir, spec)
		warnings = append(warnings, launchWarns...)
		sessionV = view
	}

	writeJSON(w, http.StatusCreated, map[string]any{
		"project": map[string]any{
			"name": folder,
			"path": projDir,
		},
		"session":    sessionV,
		"registered": register,
		"warnings":   nonNil(warnings),
	})
}

// projectSpec is the normalized create request used by the scaffold/register steps.
type projectSpec struct {
	official   string
	folder     string
	desc       string
	stack      string
	port       int
	connectors []string
	agents     []string
	related    []string
	envVars    []envVar
	created    time.Time
}

// scaffoldProject writes the standard project skeleton + the onboarding brief.
func scaffoldProject(projDir string, s projectSpec) error {
	for _, dir := range []string{projDir, filepath.Join(projDir, "docs"), filepath.Join(projDir, ".claude")} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}

	files := map[string]string{
		"README.md":               readmeMD(s),
		"CLAUDE.md":               claudeMD(s),
		"docs/PROJECT_CONTEXT.md": contextMD(s),
		"docs/ONBOARDING.md":      onboardingMD(s),
		".gitignore":              gitignore(),
		".claude/settings.json":   "{}\n",
	}
	if env := envFile(s.envVars, false); env != "" {
		files[".env"] = env
		files[".env.example"] = envFile(s.envVars, true)
	}

	for rel, content := range files {
		if err := os.WriteFile(filepath.Join(projDir, rel), []byte(content), 0o644); err != nil {
			return err
		}
	}
	return nil
}

func readmeMD(s projectSpec) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# %s\n\n", s.official)
	fmt.Fprintf(&b, "%s\n\n", s.desc)
	b.WriteString("## Status\n")
	fmt.Fprintf(&b, "New project — scaffolded via Lattice (%s). See docs/ONBOARDING.md.\n",
		s.created.Format(time.RFC3339))
	return b.String()
}

func claudeMD(s projectSpec) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# %s\n\n", s.official)
	fmt.Fprintf(&b, "Purpose: %s\n\n", s.desc)
	if s.stack != "" {
		fmt.Fprintf(&b, "Stack: %s\n\n", s.stack)
	}
	if s.port > 0 {
		fmt.Fprintf(&b, "Port: %d\n\n", s.port)
	}

	b.WriteString("## Related projects\n")
	b.WriteString(bulletList(s.related, "_none yet_"))
	b.WriteString("\n")

	b.WriteString("## Connectors / MCPs (intended)\n")
	b.WriteString(bulletList(s.connectors, "_none yet_"))
	b.WriteString("\n")

	b.WriteString("## Agents (intended)\n")
	b.WriteString(bulletList(s.agents, "_none yet_"))
	b.WriteString("\n")

	return b.String()
}

func contextMD(s projectSpec) string {
	var b strings.Builder
	b.WriteString("# Project Context\n\n")
	fmt.Fprintf(&b, "Purpose: %s\n\n", s.desc)
	b.WriteString("## Goals\n")
	b.WriteString("- Stand up the project structure for the stack\n")
	b.WriteString("- Wire the intended connectors, MCPs, and agents\n")
	b.WriteString("- Cross-link related projects\n\n")
	b.WriteString("## Stack\n")
	if s.stack != "" {
		fmt.Fprintf(&b, "%s\n\n", s.stack)
	} else {
		b.WriteString("_TBD_\n\n")
	}
	b.WriteString("## Port\n")
	if s.port > 0 {
		fmt.Fprintf(&b, "%d\n\n", s.port)
	} else {
		b.WriteString("_none_\n\n")
	}
	b.WriteString("## Created\n")
	fmt.Fprintf(&b, "%s (scaffolded via Lattice)\n", s.created.Format(time.RFC3339))
	return b.String()
}

// onboardingMD is THE BRIEF the auto-launched Claude session reads. The Setup
// tasks checklist is derived from the request so the session knows exactly what
// intent to wire.
func onboardingMD(s projectSpec) string {
	var b strings.Builder
	b.WriteString("# Onboarding\n\n")
	fmt.Fprintf(&b, "Official name: %s\n", s.official)
	fmt.Fprintf(&b, "Folder name: %s\n", s.folder)
	fmt.Fprintf(&b, "Description: %s\n", s.desc)
	if s.stack != "" {
		fmt.Fprintf(&b, "Stack: %s\n", s.stack)
	}
	if s.port > 0 {
		fmt.Fprintf(&b, "Port: %d\n", s.port)
	}
	b.WriteString("\n## Setup tasks for Claude\n")
	for _, c := range s.connectors {
		fmt.Fprintf(&b, "- [ ] Wire MCP/connector: %s\n", c)
	}
	for _, a := range s.agents {
		fmt.Fprintf(&b, "- [ ] Add agent: %s (copy/reference from ~/.claude/agents)\n", a)
	}
	for _, rp := range s.related {
		fmt.Fprintf(&b, "- [ ] Cross-link related project: %s\n", rp)
	}
	if keys := envKeys(s.envVars); keys != "" {
		fmt.Fprintf(&b, "- [ ] Confirm env vars in .env (%s)\n", keys)
	}
	b.WriteString("- [ ] Initialize project structure for the stack\n")
	b.WriteString("- [ ] Update README/CLAUDE.md as the project takes shape\n")
	b.WriteString("\nWhen done, summarize what you wired.\n")
	return b.String()
}

func gitignore() string {
	return strings.Join([]string{
		"node_modules/",
		".env",
		".env.local",
		"dist/",
		"build/",
		".DS_Store",
		"*.log",
	}, "\n") + "\n"
}

// envFile renders the env lines. blank=true masks values (the committed .example).
func envFile(vars []envVar, blank bool) string {
	if len(vars) == 0 {
		return ""
	}
	var b strings.Builder
	for _, v := range vars {
		if blank {
			fmt.Fprintf(&b, "%s=\n", v.Key)
		} else {
			fmt.Fprintf(&b, "%s=%s\n", v.Key, v.Value)
		}
	}
	return b.String()
}

func envKeys(vars []envVar) string {
	keys := make([]string, 0, len(vars))
	for _, v := range vars {
		keys = append(keys, v.Key)
	}
	return strings.Join(keys, ", ")
}

// bulletList renders a markdown bullet list, or a placeholder line when empty.
func bulletList(items []string, empty string) string {
	if len(items) == 0 {
		return "- " + empty + "\n"
	}
	var b strings.Builder
	for _, it := range items {
		fmt.Fprintf(&b, "- %s\n", it)
	}
	return b.String()
}

// gitInitProject best-effort initializes a repo + initial commit. Failure is a
// warning, never fatal (a project without git is still a valid scaffold).
func gitInitProject(projDir string) string {
	steps := [][]string{
		{"git", "init"},
		{"git", "add", "."},
		{"git", "commit", "-m", "scaffold: initial project skeleton via Lattice"},
	}
	for _, args := range steps {
		ctx, cancel := context.WithTimeout(context.Background(), gitTimeout)
		cmd := exec.CommandContext(ctx, args[0], args[1:]...)
		cmd.Dir = projDir
		out, err := cmd.CombinedOutput()
		cancel()
		if err != nil {
			return fmt.Sprintf("git %s failed (scaffold ok): %v: %s",
				args[1], err, strings.TrimSpace(string(out)))
		}
	}
	return ""
}

// launchOnboardingSession creates a Claude session in the new project and seeds
// it with the onboarding instruction. It prefers the LOCAL agent (co-located, so
// projDir already exists on disk — avoids Syncthing delay + the D23 home-path
// divergence) and otherwise falls back to placement (with a sync warning).
func (h *Hub) launchOnboardingSession(projDir string, s projectSpec) (*sessionView, []string) {
	var warnings []string

	agentID, localChosen := h.pickClaudeAgent(projDir)
	if agentID == "" {
		warnings = append(warnings, "project created; open a Claude session manually (no claude-capable agent available)")
		return nil, warnings
	}
	if !localChosen {
		warnings = append(warnings, "launched on a remote agent; the project files may need to sync (Syncthing) before Claude can read them")
	}

	// D35: the Claude session is an interactive PTY, so the onboarding brief is
	// SEEDED into it (passed in the create payload). The agent types it in once the
	// TUI has settled — a readiness check, not a blind delay — so the brief isn't
	// dropped on a slow/cold/remote box.
	rec, err := h.createOnAgent(proto.SessionClaude, "project", projDir, s.official, agentID, "", onboardingSeedPrompt, "", false)
	if err != nil {
		warnings = append(warnings, "project created; Claude session launch failed: "+err.Error())
		return nil, warnings
	}

	view := toSessionView(rec)
	return &view, warnings
}

// onboardingSeedPrompt is the single user turn injected into the new Claude
// session so it picks up the brief and finishes setup autonomously. It is seeded
// via SessionCreatePayload.SeedInput; the agent types it in once the TUI settles.
const onboardingSeedPrompt = "You've just been launched inside a freshly scaffolded project. " +
	"Read docs/ONBOARDING.md and complete the Setup tasks listed there — wire the intended " +
	"connectors/MCPs/agents, confirm env vars, set up the project structure for the stack, " +
	"and cross-link related projects. Work autonomously; when finished, summarize what you wired."

// pickClaudeAgent chooses the agent to host the onboarding session. It prefers a
// LOCAL, claude-capable, online agent; otherwise it falls back to placement
// (kind=claude). Returns the agent id ("" if none eligible) and whether the
// choice is the co-located local agent.
func (h *Hub) pickClaudeAgent(projDir string) (string, bool) {
	fleet := h.fleet()
	for _, a := range fleet {
		if a.Local && a.Online && a.Capabilities.ClaudeInstalled {
			return a.ID, true
		}
	}
	placement := ScorePlacement(PlacementRequest{
		Kind:        proto.SessionClaude,
		ProjectPath: projDir,
	}, fleet, time.Now())
	return placement.Chosen, false
}

// cleanList trims, drops empties, and de-dupes a string slice (preserving order).
func cleanList(in []string) []string {
	seen := make(map[string]struct{}, len(in))
	out := make([]string, 0, len(in))
	for _, v := range in {
		v = strings.TrimSpace(v)
		if v == "" {
			continue
		}
		if _, ok := seen[v]; ok {
			continue
		}
		seen[v] = struct{}{}
		out = append(out, v)
	}
	return out
}

// cleanEnv trims env entries and drops any with an empty key.
func cleanEnv(in []envVar) []envVar {
	out := make([]envVar, 0, len(in))
	for _, v := range in {
		k := strings.TrimSpace(v.Key)
		if k == "" {
			continue
		}
		out = append(out, envVar{Key: k, Value: v.Value})
	}
	return out
}

// nonNil returns a non-nil slice so the JSON warnings field is [] not null.
func nonNil(in []string) []string {
	if in == nil {
		return []string{}
	}
	return in
}

// portOrDash renders the registry Port column.
func portOrDash(port int) string {
	if port > 0 {
		return fmt.Sprintf("%d", port)
	}
	return "—"
}

// dashIfEmpty renders an em-dash for an empty registry cell.
func dashIfEmpty(v string) string {
	if strings.TrimSpace(v) == "" {
		return "—"
	}
	return v
}
