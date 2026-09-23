// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package gh

import (
	"context"
	"maps"
	"net/http"
	"slices"

	"github.com/google/go-github/v92/github"

	"ItsBagelBot/app/deployer/internal/ports"
	"ItsBagelBot/internal/domain/rpc/deploy"
)

// requirement decides which reported checks gate a sha.
type requirement struct {
	// names are the contexts that gate; nil means every check that did not
	// skip gates.
	names map[string]bool
	// expect must report before the set can pass: GitHub creates a check run
	// only once its workflow starts, so absence early in a PR's life means
	// "not yet", never "not needed".
	expect []string
	// codescene is folded on its own into CheckSummary.CodeScene.
	codescene string
}

func (r requirement) gates(name string, skipped bool) bool {
	if r.names == nil {
		return !skipped
	}
	return r.names[name]
}

// missing is a pending state per expected check that has not reported.
func (r requirement) missing(checks []ports.Check) []deploy.CheckState {
	seen := make(map[string]bool, len(checks))
	for _, ch := range checks {
		seen[ch.Name] = true
	}
	var out []deploy.CheckState
	for _, name := range r.expect {
		if !seen[name] {
			out = append(out, deploy.ChecksPending)
		}
	}
	return out
}

// headLookup reports whether sha is an open PR's head.
type headLookup func(ctx context.Context, sha deploy.SHA) (bool, error)

// Checks is cached per sha for cacheTTL; the Checks slice is the caller's.
func (c *Client) Checks(ctx context.Context, sha deploy.SHA) (ports.CheckSummary, error) {
	return c.checksFor(ctx, sha, c.openPRHead)
}

// knownHead is the lookup for a sha read off an open PR, which needs no call.
func knownHead(context.Context, deploy.SHA) (bool, error) { return true, nil }

func (c *Client) checksFor(ctx context.Context, sha deploy.SHA, isHead headLookup) (ports.CheckSummary, error) {
	sum, err := c.checks.load(sha, c.now(), func() (ports.CheckSummary, error) {
		return c.fetchChecks(ctx, sha, isHead)
	})
	sum.Checks = slices.Clone(sum.Checks)
	return sum, err
}

func (c *Client) fetchChecks(ctx context.Context, sha deploy.SHA, isHead headLookup) (ports.CheckSummary, error) {
	req, err := c.requirementFor(ctx, sha, isHead)
	if err != nil {
		return ports.CheckSummary{}, err
	}
	checks, err := c.reported(ctx, sha, req)
	if err != nil {
		return ports.CheckSummary{}, err
	}
	return summarize(checks, req), nil
}

// requirementFor applies main's rulesets only to an open PR's head, the one
// commit GitHub evaluates them against. Measured 2026-09-23: main's ruleset
// requires only "CodeScene Code Health Review (main)", which CodeScene posts
// on PR heads alone; origin/main's head carried 13 check runs and no
// CodeScene, so expecting it there held preflight pending forever. A pushed
// commit is gated instead by every check that ran on it and did not skip.
func (c *Client) requirementFor(ctx context.Context, sha deploy.SHA, isHead headLookup) (requirement, error) {
	head, err := isHead(ctx, sha)
	if err != nil || !head {
		return requirement{codescene: c.cfg.Deploy.CodeSceneCheck}, err
	}
	return c.required.load(c.cfg.Deploy.MainBranch, c.now(), func() (requirement, error) {
		return c.fetchRequired(ctx)
	})
}

// openPRHead reads the PRs containing sha. For a commit already on main that
// is only the merged PR that introduced it, whose head is another sha.
func (c *Client) openPRHead(ctx context.Context, sha deploy.SHA) (bool, error) {
	prs, resp, err := c.gh.PullRequests.ListPullRequestsWithCommit(ctx, c.owner, c.repo, string(sha),
		&github.ListOptions{PerPage: 100})
	if err != nil {
		return false, apiErr(resp, err)
	}
	return slices.ContainsFunc(prs, func(pr *github.PullRequest) bool {
		return pr.GetState() == "open" && pr.GetHead().GetSHA() == string(sha)
	}), nil
}

// rulesUnavailable are the answers where the ruleset endpoint cannot say
// (rulesets not on the plan, or the endpoint hidden from the App). They fall
// back to gating on every check that did not skip, a superset of what any
// ruleset requires, so a degraded answer only ever makes the train stricter.
var rulesUnavailable = map[int]bool{http.StatusForbidden: true, http.StatusNotFound: true}

// fetchRequired reads the required status checks from the rulesets that
// apply to main. Classic branch protection is not read: it needs the
// administration permission, which the App deliberately lacks. CodeScene is
// expected ruleset or not: merge_prs waits on it by name.
func (c *Client) fetchRequired(ctx context.Context) (requirement, error) {
	cs := c.cfg.Deploy.CodeSceneCheck
	req := requirement{expect: []string{cs}, codescene: cs}
	rules, resp, err := c.gh.Repositories.ListRulesForBranch(ctx, c.owner, c.repo,
		string(c.cfg.Deploy.MainBranch), &github.ListOptions{PerPage: 100})
	if rulesUnavailable[statusOf(resp)] {
		return req, nil
	}
	if err != nil {
		return req, apiErr(resp, err)
	}
	names := requiredNames(rules)
	if len(names) > 0 {
		names[cs] = true
		req.names = names
		req.expect = slices.Sorted(maps.Keys(names))
	}
	return req, nil
}

func requiredNames(rules *github.BranchRules) map[string]bool {
	names := map[string]bool{}
	for _, rule := range rules.GetRequiredStatusChecks() {
		for _, check := range rule.Parameters.RequiredStatusChecks {
			names[check.Context] = true
		}
	}
	return names
}

// reported merges check runs (Actions jobs, CodeScene) with commit statuses
// (older integrations that post statuses rather than check runs).
func (c *Client) reported(ctx context.Context, sha deploy.SHA, req requirement) ([]ports.Check, error) {
	opts := &github.ListCheckRunsOptions{Filter: github.Ptr("latest"), ListOptions: github.ListOptions{PerPage: 100}}
	runs, err := collect(&opts.ListOptions, func() ([]*github.CheckRun, *github.Response, error) {
		res, resp, err := c.gh.Checks.ListCheckRunsForRef(ctx, c.owner, c.repo, string(sha), opts)
		return res.GetCheckRuns(), resp, err
	})
	if err != nil {
		return nil, err
	}
	combined, resp, err := c.gh.Repositories.GetCombinedStatus(ctx, c.owner, c.repo, string(sha),
		&github.ListOptions{PerPage: 100})
	if err != nil {
		return nil, apiErr(resp, err)
	}
	checks := make([]ports.Check, 0, len(runs)+len(combined.GetStatuses()))
	for _, run := range runs {
		checks = append(checks, ports.Check{
			Name: run.GetName(), State: runState(run), URL: run.GetHTMLURL(),
			Required: req.gates(run.GetName(), run.GetConclusion() == "skipped"),
			Summary:  failedSummary(run),
		})
	}
	for _, st := range combined.GetStatuses() {
		checks = append(checks, ports.Check{
			Name: st.GetContext(), State: statusState(st), URL: st.GetTargetURL(),
			Required: req.gates(st.GetContext(), false),
		})
	}
	return checks, nil
}

// passing conclusions. neutral and skipped pass: a skipped job is a path
// filter or an if: saying there is nothing to check, which is how this
// repo's workflows report a no-op.
var passing = map[string]bool{"success": true, "neutral": true, "skipped": true}

func runState(run *github.CheckRun) deploy.CheckState {
	switch {
	case run.GetStatus() != "completed":
		return deploy.ChecksPending
	case passing[run.GetConclusion()]:
		return deploy.ChecksSuccess
	}
	return deploy.ChecksFailure
}

// failedSummary is a red run's output.summary. A green run's summary is
// dropped so a passing CodeScene report never lands in a failure's log tail.
func failedSummary(run *github.CheckRun) string {
	if runState(run) != deploy.ChecksFailure {
		return ""
	}
	return run.GetOutput().GetSummary()
}

// statusState: a commit status is success, pending, failure or error.
func statusState(st *github.RepoStatus) deploy.CheckState {
	switch st.GetState() {
	case "success":
		return deploy.ChecksSuccess
	case "pending":
		return deploy.ChecksPending
	}
	return deploy.ChecksFailure
}

func summarize(checks []ports.Check, req requirement) ports.CheckSummary {
	var gating, codescene []deploy.CheckState
	for _, ch := range checks {
		if ch.Required {
			gating = append(gating, ch.State)
		}
		if ch.Name == req.codescene {
			codescene = append(codescene, ch.State)
		}
	}
	gating = append(gating, req.missing(checks)...)
	return ports.CheckSummary{State: fold(gating), CodeScene: fold(codescene), Checks: checks}
}

// severity ranks failure over pending over success: one red check fails the
// set while others still run, so the train stops waiting at the first red.
var severity = map[deploy.CheckState]int{
	deploy.ChecksNone:    0,
	deploy.ChecksSuccess: 1,
	deploy.ChecksPending: 2,
	deploy.ChecksFailure: 3,
}

func fold(states []deploy.CheckState) deploy.CheckState {
	out := deploy.ChecksNone
	for _, s := range states {
		if severity[s] > severity[out] {
			out = s
		}
	}
	return out
}
