package hub

import (
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/shleesauce/lattice/internal/proto"
)

// Workflow templates (E, v0.1.5): paste a GitHub issue or PR URL → Lattice spins up
// a scoped Claude session pre-briefed to do the work in a dedicated git worktree
// (issue-<n> / review-<n>), auto-placed on the best machine. It rides the EXISTING
// session machinery: placement (ScorePlacement), createOnAgent, and the SeedInput
// onboarding-brief mechanism (the agent types the brief in once the TUI settles).
// The worktree is created by the seeded first turn — claude has the repo + shell +
// gh on the chosen box — exactly like project onboarding delegates its setup to the
// seeded turn, rather than the hub reaching into a remote machine's git.

// issueURLRe / prURLRe parse a GitHub issue or pull-request URL into owner, repo, n.
var (
	wfIssueRe = regexp.MustCompile(`^https://github\.com/([A-Za-z0-9_.-]+)/([A-Za-z0-9_.-]+)/issues/(\d+)`)
	wfPRRe    = regexp.MustCompile(`^https://github\.com/([A-Za-z0-9_.-]+)/([A-Za-z0-9_.-]+)/pull/(\d+)`)
)

// workflowKind is the template applied.
type workflowKind string

const (
	workflowImplementIssue workflowKind = "implement_issue"
	workflowReviewPR       workflowKind = "review_pr"
)

// parsedWorkflow is a GitHub URL resolved to its template + coordinates.
type parsedWorkflow struct {
	kind   workflowKind
	owner  string
	repo   string
	number string
	url    string
}

// parseWorkflowURL classifies a GitHub issue/PR URL. ok=false for anything else.
func parseWorkflowURL(raw string) (parsedWorkflow, bool) {
	raw = strings.TrimSpace(raw)
	if m := wfIssueRe.FindStringSubmatch(raw); m != nil {
		return parsedWorkflow{kind: workflowImplementIssue, owner: m[1], repo: m[2], number: m[3], url: canonicalURL(raw, m[0])}, true
	}
	if m := wfPRRe.FindStringSubmatch(raw); m != nil {
		return parsedWorkflow{kind: workflowReviewPR, owner: m[1], repo: m[2], number: m[3], url: canonicalURL(raw, m[0])}, true
	}
	return parsedWorkflow{}, false
}

// canonicalURL trims a matched URL to the canonical issue/PR form (drops trailing
// path/query like /files).
func canonicalURL(raw, matched string) string {
	if matched != "" {
		return matched
	}
	return raw
}

// worktreeBranch is the dedicated branch/worktree name for a workflow:
// issue-<n> for an issue, review-<n> for a PR.
func (p parsedWorkflow) worktreeBranch() string {
	if p.kind == workflowReviewPR {
		return "review-" + p.number
	}
	return "issue-" + p.number
}

// title is a short session title for the workflow.
func (p parsedWorkflow) title() string {
	if p.kind == workflowReviewPR {
		return "review PR #" + p.number
	}
	return "issue #" + p.number
}

// createWorkflowBody is the POST /api/workflows request. url is the GitHub issue/PR
// link; projectPath is the local clone of that repo (chosen with the same project
// picker as New Session). Optional pin/permission/model/notify mirror New Session.
type createWorkflowBody struct {
	URL            string `json:"url"`
	ProjectPath    string `json:"projectPath"`
	UserAgentID    string `json:"userAgentId"`
	PinAgentID     string `json:"pinAgentId"`
	PermissionMode string `json:"permissionMode"`
	Model          string `json:"model"`
	NotifyOnIdle   bool   `json:"notifyOnIdle"`
}

// handleWorkflows answers POST /api/workflows: parse the URL, build the guardrail
// brief + worktree name, place the session, and create a pre-briefed Claude session.
func (h *Hub) handleWorkflows(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var body createWorkflowBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid json body"})
		return
	}
	wf, ok := parseWorkflowURL(body.URL)
	if !ok {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "not a GitHub issue or pull-request URL"})
		return
	}
	projectPath := strings.TrimSpace(body.ProjectPath)
	if projectPath == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "projectPath is required (the local clone of this repo)"})
		return
	}

	// Place exactly like a project Claude session (D32 default-pin to the primary
	// machine when the caller didn't pick one).
	pin := body.PinAgentID
	if pin == "" {
		pin = h.defaultPin()
	}
	req := PlacementRequest{
		Kind:        proto.SessionClaude,
		ProjectPath: projectPath,
		UserAgentID: body.UserAgentID,
		PinAgentID:  pin,
	}
	placement := ScorePlacement(req, h.fleet(), time.Now())
	if placement.Chosen == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error":     "no eligible agent for this workflow session",
			"placement": placement,
		})
		return
	}

	seed := workflowSeedPrompt(wf)
	rec, err := h.createOnAgent(
		proto.SessionClaude, "project", projectPath, wf.title(),
		placement.Chosen, "", seed, body.PermissionMode,
		body.NotifyOnIdle, strings.TrimSpace(body.Model), false,
	)
	if err != nil {
		writeJSON(w, statusForRoundTrip(err), map[string]any{"error": err.Error(), "placement": placement})
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{
		"session":   toSessionView(rec),
		"placement": placement,
		"workflow":  map[string]string{"kind": string(wf.kind), "url": wf.url, "worktree": wf.worktreeBranch()},
	})
}

// workflowSeedPrompt builds the guardrail brief typed into the new Claude session.
// It tells claude to create the dedicated worktree FIRST (so the work is isolated on
// its own branch), then do the scoped task — implement the issue, or review the PR.
// The brief is deliberately tight: it pins the worktree name and the scope so the
// unattended session stays on-task.
func workflowSeedPrompt(wf parsedWorkflow) string {
	branch := wf.worktreeBranch()
	// Worktree placed as a sibling of the repo so it doesn't nest inside it.
	worktreePath := fmt.Sprintf("../%s-%s", wf.repo, branch)

	if wf.kind == workflowReviewPR {
		return strings.Join([]string{
			fmt.Sprintf("You're reviewing GitHub pull request %s.", wf.url),
			fmt.Sprintf("First, set up an isolated review worktree: run `git worktree add -B %s %s` (if that branch/worktree already exists, just `cd %s` into it), then check out the PR head with `gh pr checkout %s`.", branch, worktreePath, worktreePath, wf.number),
			"Then do a thorough code review: read the diff, run the build and tests, and flag bugs, security issues, and missing edge cases.",
			"Summarize your findings clearly. Do NOT merge or push anything — leave that to the human. Stay scoped to this PR.",
		}, " ")
	}
	return strings.Join([]string{
		fmt.Sprintf("You're implementing GitHub issue %s.", wf.url),
		fmt.Sprintf("First, read the issue with `gh issue view %s`, then set up an isolated worktree: run `git worktree add -B %s %s` (if it already exists, `cd %s` into it).", wf.number, branch, worktreePath, worktreePath),
		"Then implement the change on that branch: write the code, add or update tests, and verify the build and tests pass.",
		"When done, summarize what you changed. Do NOT push or open a PR unless the human asks. Stay scoped to this issue.",
	}, " ")
}
