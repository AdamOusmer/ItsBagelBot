// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package gh

import (
	"context"
	"fmt"
	"slices"

	"github.com/google/go-github/v92/github"
	"golang.org/x/sync/errgroup"

	"ItsBagelBot/app/deployer/internal/ports"
	"ItsBagelBot/internal/domain/rpc/deploy"
)

func toPR(pr *github.PullRequest) ports.PullRequest {
	out := ports.PullRequest{
		Number:     pr.GetNumber(),
		Title:      pr.GetTitle(),
		Author:     pr.GetUser().GetLogin(),
		URL:        pr.GetHTMLURL(),
		HeadBranch: ports.Branch(pr.GetHead().GetRef()),
		HeadSHA:    deploy.SHA(pr.GetHead().GetSHA()),
		BaseBranch: ports.Branch(pr.GetBase().GetRef()),
		Draft:      pr.GetDraft(),
		Open:       pr.GetState() == "open",
		// merged_at, unlike merged, is on the list endpoint too.
		Merged:    pr.MergedAt != nil,
		Mergeable: pr.Mergeable,
		Behind:    pr.GetMergeableState() == "behind",
	}
	if out.Merged {
		out.MergeSHA = deploy.SHA(pr.GetMergeCommitSHA())
	}
	return out
}

// OpenPRs is cached for cacheTTL; the slice is the caller's to keep.
func (c *Client) OpenPRs(ctx context.Context) ([]deploy.PRInfo, error) {
	infos, err := c.openPRs.load(c.cfg.Deploy.MainBranch, c.now(), func() ([]deploy.PRInfo, error) {
		return c.fetchOpenPRs(ctx)
	})
	return slices.Clone(infos), err
}

func (c *Client) fetchOpenPRs(ctx context.Context) ([]deploy.PRInfo, error) {
	opts := &github.PullRequestListOptions{
		State: "open", Base: string(c.cfg.Deploy.MainBranch), ListOptions: github.ListOptions{PerPage: 100},
	}
	prs, err := collect(&opts.ListOptions, func() ([]*github.PullRequest, *github.Response, error) {
		return c.gh.PullRequests.List(ctx, c.owner, c.repo, opts)
	})
	if err != nil {
		return nil, err
	}
	infos := make([]deploy.PRInfo, len(prs))
	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(fanout)
	for i, pr := range prs {
		g.Go(func() (err error) {
			infos[i], err = c.prInfo(gctx, pr.GetNumber())
			return err
		})
	}
	if err := g.Wait(); err != nil {
		return nil, err
	}
	return infos, nil
}

// prInfo re-reads the PR because the list endpoint omits mergeable and
// mergeable_state, which only the single-PR endpoint computes. The head of a
// listed open PR is known to be one, so its checks skip the head lookup.
func (c *Client) prInfo(ctx context.Context, number int) (deploy.PRInfo, error) {
	pr, err := c.PR(ctx, number)
	if err != nil {
		return deploy.PRInfo{}, err
	}
	sum, err := c.checksFor(ctx, pr.HeadSHA, knownHead)
	if err != nil {
		return deploy.PRInfo{}, err
	}
	return deploy.PRInfo{
		Number:    pr.Number,
		Title:     pr.Title,
		Author:    pr.Author,
		URL:       pr.URL,
		HeadSHA:   pr.HeadSHA,
		Draft:     pr.Draft,
		Checks:    sum.State,
		CodeScene: sum.CodeScene,
		Mergeable: pr.Mergeable != nil && *pr.Mergeable,
		Behind:    pr.Behind,
	}, nil
}

func (c *Client) PR(ctx context.Context, number int) (ports.PullRequest, error) {
	pr, resp, err := c.gh.PullRequests.Get(ctx, c.owner, c.repo, number)
	if err != nil {
		return ports.PullRequest{}, apiErr(resp, err)
	}
	return toPR(pr), nil
}

// FindPR re-reads the chosen PR so Mergeable and Behind are filled, which
// the list endpoint leaves out.
func (c *Client) FindPR(ctx context.Context, head ports.Branch) (ports.PullRequest, bool, error) {
	prs, resp, err := c.gh.PullRequests.List(ctx, c.owner, c.repo, &github.PullRequestListOptions{
		State:       "all",
		Head:        c.owner + ":" + string(head),
		Base:        string(c.cfg.Deploy.MainBranch),
		ListOptions: github.ListOptions{PerPage: 100},
	})
	if err != nil {
		return ports.PullRequest{}, false, apiErr(resp, err)
	}
	number := pickPR(prs)
	if number == 0 {
		return ports.PullRequest{}, false, nil
	}
	pr, err := c.PR(ctx, number)
	return pr, err == nil, err
}

// pickPR prefers the branch's open PR, then its most recently created merged
// one (the list is newest first). A closed, unmerged PR is an abandoned
// attempt and never counts.
func pickPR(prs []*github.PullRequest) int {
	best, bestRank := 0, 0
	for _, pr := range prs {
		if r := prRank(pr); r > bestRank {
			best, bestRank = pr.GetNumber(), r
		}
	}
	return best
}

func prRank(pr *github.PullRequest) int {
	switch {
	case pr.GetState() == "open":
		return 2
	case pr.MergedAt != nil:
		return 1
	}
	return 0
}

func (c *Client) CreatePR(ctx context.Context, spec ports.PRSpec) (ports.PullRequest, error) {
	pr, resp, err := c.gh.PullRequests.Create(ctx, c.owner, c.repo, github.CreatePullRequest{
		Title: github.Ptr(spec.Title), Head: string(spec.Head), Base: string(spec.Base), Body: github.Ptr(spec.Body),
	})
	if err != nil {
		return ports.PullRequest{}, unprocessable(resp, err, ports.ErrConflict)
	}
	return toPR(pr), nil
}

// UpdateBranch merges main into the PR head. GitHub does it in the
// background and answers 202, which is success here; the caller re-waits on
// the new head's checks.
func (c *Client) UpdateBranch(ctx context.Context, number int) error {
	_, resp, err := c.gh.PullRequests.UpdateBranch(ctx, c.owner, c.repo, number, nil)
	if accepted(err) {
		return nil
	}
	return apiErr(resp, err)
}

// SquashMerge sets the commit title explicitly: GitHub's squash default
// for a one-commit PR is that commit's subject, not the PR title, and the
// repo convention is "<title> (#N)", which the plan's commit list reads PR
// numbers from. An already merged PR returns its merge commit, so a resumed
// stage does not fail on its own earlier success.
func (c *Client) SquashMerge(ctx context.Context, number int, expectHead deploy.SHA) (deploy.SHA, error) {
	pr, err := c.PR(ctx, number)
	if err != nil {
		return "", err
	}
	if pr.Merged {
		return pr.MergeSHA, nil
	}
	res, resp, err := c.gh.PullRequests.Merge(ctx, c.owner, c.repo, number, "", &github.PullRequestOptions{
		CommitTitle: fmt.Sprintf("%s (#%d)", pr.Title, number),
		SHA:         string(expectHead),
		MergeMethod: "squash",
	})
	if err != nil {
		return "", apiErr(resp, err)
	}
	if !res.GetMerged() {
		return "", fmt.Errorf("merge #%d: %s: %w", number, res.GetMessage(), ports.ErrConflict)
	}
	return deploy.SHA(res.GetSHA()), nil
}
