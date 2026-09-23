// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package gh

import (
	"context"
	"encoding/base64"
	"fmt"
	"maps"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/google/go-github/v92/github"

	"ItsBagelBot/app/deployer/internal/ports"
	"ItsBagelBot/internal/domain/rpc/deploy"
)

// gitRef is a ref path under refs/, e.g. "heads/main" or "tags/v0.2.3-beta".
type gitRef string

func headRef(b ports.Branch) gitRef  { return gitRef("heads/" + b) }
func tagRef(v deploy.Version) gitRef { return gitRef("tags/" + v) }

func (c *Client) BranchHead(ctx context.Context, branch ports.Branch) (deploy.SHA, error) {
	ref, resp, err := c.gh.Git.GetRef(ctx, c.owner, c.repo, string(headRef(branch)))
	if err != nil {
		return "", apiErr(resp, err)
	}
	return deploy.SHA(ref.GetObject().GetSHA()), nil
}

// betaTag is the release tag shape; LatestTag ignores every other tag.
var betaTag = regexp.MustCompile(`^v(\d+)\.(\d+)\.(\d+)-beta$`)

type semver [3]int

func parseBeta(ref *github.Reference) (semver, bool) {
	m := betaTag.FindStringSubmatch(tagName(ref))
	if m == nil {
		return semver{}, false
	}
	var v semver
	for i := range v {
		v[i], _ = strconv.Atoi(m[i+1])
	}
	return v, true
}

func (v semver) less(o semver) bool { return slices.Compare(v[:], o[:]) < 0 }

func tagName(ref *github.Reference) string {
	return strings.TrimPrefix(ref.GetRef(), "refs/tags/")
}

// LatestTag compares by semver, not by name or date: v0.10.0-beta sorts
// before v0.9.0-beta as a string, and a hotfix force-move rewrites dates.
func (c *Client) LatestTag(ctx context.Context) (ports.TagRef, error) {
	refs, resp, err := c.gh.Git.ListMatchingRefs(ctx, c.owner, c.repo, "tags/v")
	if err != nil {
		return ports.TagRef{}, apiErr(resp, err)
	}
	var best *github.Reference
	bestV := semver{-1, -1, -1}
	for _, ref := range refs {
		if v, ok := parseBeta(ref); ok && bestV.less(v) {
			best, bestV = ref, v
		}
	}
	if best == nil {
		return ports.TagRef{}, fmt.Errorf("no vX.Y.Z-beta tag: %w", ports.ErrNotFound)
	}
	return c.peel(ctx, best)
}

func (c *Client) Tag(ctx context.Context, name deploy.Version) (ports.TagRef, bool, error) {
	ref, resp, err := c.gh.Git.GetRef(ctx, c.owner, c.repo, string(tagRef(name)))
	if ok, err := found(resp, err); !ok {
		return ports.TagRef{}, false, err
	}
	tag, err := c.peel(ctx, ref)
	return tag, err == nil, err
}

// peel resolves a tag ref to its commit: an annotated tag's ref points at a
// tag object, whose own object is the commit.
func (c *Client) peel(ctx context.Context, ref *github.Reference) (ports.TagRef, error) {
	obj := ref.GetObject()
	out := ports.TagRef{
		Name:      deploy.Version(tagName(ref)),
		CommitSHA: deploy.SHA(obj.GetSHA()),
		ObjectSHA: deploy.SHA(obj.GetSHA()),
	}
	if obj.GetType() != "tag" {
		return out, nil
	}
	tag, resp, err := c.gh.Git.GetTag(ctx, c.owner, c.repo, obj.GetSHA())
	if err != nil {
		return ports.TagRef{}, apiErr(resp, err)
	}
	out.CommitSHA = deploy.SHA(tag.GetObject().GetSHA())
	return out, nil
}

// UpsertTag is a no-op when the tag already names spec.Commit, so a resumed
// tag stage does not push the ref again and start a second publish-images
// run for the same commit.
func (c *Client) UpsertTag(ctx context.Context, spec ports.TagSpec) (ports.TagRef, error) {
	cur, exists, err := c.Tag(ctx, spec.Name)
	if err != nil {
		return ports.TagRef{}, err
	}
	if exists && cur.CommitSHA == spec.Commit {
		return cur, nil
	}
	if exists && !spec.Force {
		return ports.TagRef{}, fmt.Errorf("tag %s is at %s, not %s: %w", spec.Name, cur.CommitSHA, spec.Commit, ports.ErrConflict)
	}
	obj, resp, err := c.gh.Git.CreateTag(ctx, c.owner, c.repo, github.CreateTag{
		Tag: string(spec.Name), Message: spec.Message, Object: string(spec.Commit), Type: "commit",
	})
	if err != nil {
		return ports.TagRef{}, apiErr(resp, err)
	}
	out := ports.TagRef{Name: spec.Name, CommitSHA: spec.Commit, ObjectSHA: deploy.SHA(obj.GetSHA())}
	if err := c.pointRef(ctx, tagRef(spec.Name), out.ObjectSHA, exists); err != nil {
		return ports.TagRef{}, err
	}
	return out, nil
}

// pointRef creates ref at sha, or force-moves it when it exists. Both go
// through the App token, so the resulting push event starts workflows (a
// GITHUB_TOKEN push would not).
func (c *Client) pointRef(ctx context.Context, ref gitRef, sha deploy.SHA, exists bool) error {
	if exists {
		_, resp, err := c.gh.Git.UpdateRef(ctx, c.owner, c.repo, string(ref),
			github.UpdateRef{SHA: string(sha), Force: github.Ptr(true)})
		return apiErr(resp, err)
	}
	_, resp, err := c.gh.Git.CreateRef(ctx, c.owner, c.repo, github.CreateRef{Ref: "refs/" + string(ref), SHA: string(sha)})
	return unprocessable(resp, err, ports.ErrConflict)
}

func (c *Client) Compare(ctx context.Context, base, head ports.Ref) (ports.Comparison, error) {
	var out ports.Comparison
	files := map[ports.FilePath]bool{}
	list := &github.ListOptions{PerPage: 100}
	commits, err := collect(list, func() ([]*github.RepositoryCommit, *github.Response, error) {
		cmp, resp, err := c.gh.Repositories.CompareCommits(ctx, c.owner, c.repo, string(base), string(head), list)
		out.AheadBy, out.BehindBy = cmp.GetAheadBy(), cmp.GetBehindBy()
		for _, f := range cmp.GetFiles() {
			files[ports.FilePath(f.GetFilename())] = true
		}
		return cmp.GetCommits(), resp, err
	})
	if err != nil {
		return ports.Comparison{}, err
	}
	out.Commits = mapAll(commits, toCommit)
	out.Files = slices.Sorted(maps.Keys(files))
	return out, nil
}

// prRef finds the PR number in a commit subject: "(#N)" at the end of a
// squash merge, or "Merge pull request #N " on a merge commit.
var prRef = regexp.MustCompile(`^Merge pull request #(\d+) |\(#(\d+)\)$`)

func toCommit(rc *github.RepositoryCommit) deploy.Commit {
	title, _, _ := strings.Cut(rc.GetCommit().GetMessage(), "\n")
	author := rc.GetAuthor().GetLogin()
	if author == "" {
		author = rc.GetCommit().GetAuthor().GetName()
	}
	out := deploy.Commit{SHA: deploy.SHA(rc.GetSHA()), Title: title, Author: author, URL: rc.GetHTMLURL()}
	if m := prRef.FindStringSubmatch(title); m != nil {
		out.PR, _ = strconv.Atoi(m[1] + m[2])
	}
	return out
}

// DeleteBranch returns ErrNotFound for a branch already gone (GitHub's
// auto-delete of merged heads may have beaten the train to it).
func (c *Client) DeleteBranch(ctx context.Context, branch ports.Branch) error {
	resp, err := c.gh.Git.DeleteRef(ctx, c.owner, c.repo, string(headRef(branch)))
	return unprocessable(resp, err, ports.ErrNotFound)
}

func (c *Client) CommitFiles(ctx context.Context, spec ports.CommitSpec) (deploy.SHA, error) {
	base, resp, err := c.gh.Git.GetCommit(ctx, c.owner, c.repo, string(spec.Base))
	if err != nil {
		return "", apiErr(resp, err)
	}
	entries, err := c.blobEntries(ctx, spec.Files)
	if err != nil {
		return "", err
	}
	tree, resp, err := c.gh.Git.CreateTree(ctx, c.owner, c.repo, base.GetTree().GetSHA(), entries)
	if err != nil {
		return "", apiErr(resp, err)
	}
	commit, resp, err := c.gh.Git.CreateCommit(ctx, c.owner, c.repo, github.Commit{
		Message: github.Ptr(spec.Message),
		Tree:    &github.Tree{SHA: tree.SHA},
		Parents: []*github.Commit{{SHA: base.SHA}},
	}, nil)
	if err != nil {
		return "", apiErr(resp, err)
	}
	sha := deploy.SHA(commit.GetSHA())
	if err := c.pointRef(ctx, headRef(spec.Branch), sha, false); err != nil {
		return "", err
	}
	return sha, nil
}

// blobEntries uploads each file as a base64 blob. A tree entry can carry
// inline content, but only as a JSON string, so only valid UTF-8 survives;
// a blob keeps the bytes exact, and a pin PR is 15 files, so the extra
// calls cost nothing that matters.
func (c *Client) blobEntries(ctx context.Context, files ports.Files) ([]*github.TreeEntry, error) {
	entries := make([]*github.TreeEntry, 0, len(files))
	for _, p := range slices.Sorted(maps.Keys(files)) {
		blob, resp, err := c.gh.Git.CreateBlob(ctx, c.owner, c.repo, github.Blob{
			Content:  github.Ptr(base64.StdEncoding.EncodeToString(files[p])),
			Encoding: github.Ptr("base64"),
		})
		if err != nil {
			return nil, apiErr(resp, err)
		}
		entries = append(entries, &github.TreeEntry{
			Path: github.Ptr(string(p)), Mode: github.Ptr("100644"), Type: github.Ptr("blob"), SHA: blob.SHA,
		})
	}
	return entries, nil
}
