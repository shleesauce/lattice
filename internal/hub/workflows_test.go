package hub

import (
	"strings"
	"testing"
)

// parseWorkflowURL classifies issue vs PR URLs, extracts owner/repo/number, trims
// trailing path, and rejects non-issue/PR URLs.
func TestParseWorkflowURL(t *testing.T) {
	issue, ok := parseWorkflowURL("https://github.com/shleesauce/lattice/issues/42")
	if !ok || issue.kind != workflowImplementIssue {
		t.Fatalf("issue parse: ok=%v kind=%v", ok, issue.kind)
	}
	if issue.owner != "shleesauce" || issue.repo != "lattice" || issue.number != "42" {
		t.Fatalf("issue fields: %+v", issue)
	}
	if issue.worktreeBranch() != "issue-42" {
		t.Fatalf("issue worktree: %q", issue.worktreeBranch())
	}

	pr, ok := parseWorkflowURL("https://github.com/shleesauce/lattice/pull/7/files")
	if !ok || pr.kind != workflowReviewPR {
		t.Fatalf("pr parse: ok=%v kind=%v", ok, pr.kind)
	}
	if pr.number != "7" || pr.worktreeBranch() != "review-7" {
		t.Fatalf("pr fields: %+v worktree=%q", pr, pr.worktreeBranch())
	}
	// canonical URL drops the /files suffix
	if pr.url != "https://github.com/shleesauce/lattice/pull/7" {
		t.Fatalf("pr url not canonicalized: %q", pr.url)
	}

	for _, bad := range []string{
		"", "https://github.com/o/r", "https://github.com/o/r/commit/abc",
		"https://gitlab.com/o/r/issues/1", "not a url",
	} {
		if _, ok := parseWorkflowURL(bad); ok {
			t.Errorf("parseWorkflowURL(%q) should be rejected", bad)
		}
	}
}

// The seed prompt pins the worktree name, the issue/PR number, and the guardrails
// (no push/merge for review; scoped work for issue).
func TestWorkflowSeedPrompt(t *testing.T) {
	issue, _ := parseWorkflowURL("https://github.com/o/lattice/issues/42")
	si := workflowSeedPrompt(issue)
	for _, want := range []string{"issue", "42", "git worktree add", "issue-42", "../lattice-issue-42", "tests"} {
		if !strings.Contains(si, want) {
			t.Errorf("issue seed missing %q:\n%s", want, si)
		}
	}
	if strings.Contains(si, "gh pr checkout") {
		t.Errorf("issue seed should not mention PR checkout:\n%s", si)
	}

	pr, _ := parseWorkflowURL("https://github.com/o/lattice/pull/7")
	sp := workflowSeedPrompt(pr)
	for _, want := range []string{"pull request", "review-7", "gh pr checkout 7", "Do NOT merge or push"} {
		if !strings.Contains(sp, want) {
			t.Errorf("pr seed missing %q:\n%s", want, sp)
		}
	}
}
