// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package gh

import (
	"context"

	"github.com/google/go-github/v92/github"

	"ItsBagelBot/app/deployer/internal/ports"
	"ItsBagelBot/internal/domain/rpc/deploy"
)

func toRelease(rel *github.RepositoryRelease, latestID int64) ports.Release {
	return ports.Release{
		ID:              rel.GetID(),
		Tag:             deploy.Version(rel.GetTagName()),
		Title:           rel.GetName(),
		URL:             rel.GetHTMLURL(),
		TargetCommitish: rel.GetTargetCommitish(),
		Latest:          rel.GetID() == latestID,
		PublishedAt:     rel.GetPublishedAt().Time,
	}
}

// latestReleaseID is 0 when the repo has no release yet. A release object
// does not say whether it is Latest; only this endpoint does.
func (c *Client) latestReleaseID(ctx context.Context) (int64, error) {
	rel, resp, err := c.gh.Repositories.GetLatestRelease(ctx, c.owner, c.repo)
	_, err = found(resp, err)
	return rel.GetID(), err
}

func (c *Client) Release(ctx context.Context, tag deploy.Version) (ports.Release, bool, error) {
	rel, resp, err := c.gh.Repositories.GetReleaseByTag(ctx, c.owner, c.repo, string(tag))
	if ok, err := found(resp, err); !ok {
		return ports.Release{}, false, err
	}
	latest, err := c.latestReleaseID(ctx)
	if err != nil {
		return ports.Release{}, false, err
	}
	return toRelease(rel, latest), true, nil
}

// UpsertRelease writes prerelease false on both paths: GitHub refuses to
// mark a prerelease Latest, and the -beta suffix is the product's version
// scheme, not a prerelease flag. On update target_commitish is re-pointed
// for a hotfix; GitHub ignores it when the tag exists, so the tag's own
// force-move is what moves the release.
func (c *Client) UpsertRelease(ctx context.Context, spec ports.ReleaseSpec) (ports.Release, error) {
	cur, resp, err := c.gh.Repositories.GetReleaseByTag(ctx, c.owner, c.repo, string(spec.Tag))
	exists, err := found(resp, err)
	if err != nil {
		return ports.Release{}, err
	}
	var rel *github.RepositoryRelease
	if exists {
		rel, resp, err = c.gh.Repositories.UpdateRelease(ctx, c.owner, c.repo, cur.GetID(), github.UpdateReleaseRequest{
			Name: github.Ptr(spec.Title), Body: github.Ptr(spec.Notes), TargetCommitish: github.Ptr(string(spec.Target)),
			Prerelease: github.Ptr(false), MakeLatest: github.Ptr("true"),
		})
	} else {
		rel, resp, err = c.gh.Repositories.CreateRelease(ctx, c.owner, c.repo, github.CreateReleaseRequest{
			TagName: string(spec.Tag), Name: github.Ptr(spec.Title), Body: github.Ptr(spec.Notes),
			TargetCommitish: github.Ptr(string(spec.Target)), Prerelease: github.Ptr(false), MakeLatest: github.Ptr("true"),
		})
	}
	if err != nil {
		return ports.Release{}, apiErr(resp, err)
	}
	return toRelease(rel, rel.GetID()), nil
}

// Releases reads one page: the Deploys page offers the last few releases as
// rollback targets, far below GitHub's 100 per page. Drafts are skipped.
func (c *Client) Releases(ctx context.Context, limit int) ([]ports.Release, error) {
	rels, resp, err := c.gh.Repositories.ListReleases(ctx, c.owner, c.repo, &github.ListOptions{PerPage: min(limit, 100)})
	if err != nil {
		return nil, apiErr(resp, err)
	}
	latest, err := c.latestReleaseID(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]ports.Release, 0, len(rels))
	for _, rel := range rels {
		if !rel.GetDraft() {
			out = append(out, toRelease(rel, latest))
		}
	}
	return out, nil
}
