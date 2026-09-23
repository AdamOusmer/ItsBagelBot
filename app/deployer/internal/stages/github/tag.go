// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package github

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"ItsBagelBot/app/deployer/internal/ports"
	"ItsBagelBot/app/deployer/internal/stage"
	"ItsBagelBot/internal/domain/rpc/deploy"
)

// tag: the annotated version tag, created through the App token so its push
// triggers publish-images (a push made with the workflow's own GITHUB_TOKEN
// would not start another workflow).

func tagDone(ctx context.Context, rc *stage.RunCtx) (bool, error) {
	run := rc.View()
	ref, found, err := rc.Deps.GitHub.Tag(ctx, run.Version)
	if err != nil || !found {
		return false, err
	}
	if ref.CommitSHA != tagTarget(run) {
		return false, nil
	}
	return true, rc.SetOutputs(ctx, func(o *deploy.Outputs) { o.TagSHA = ref.CommitSHA })
}

func runTag(ctx context.Context, rc *stage.RunCtx) error {
	run := rc.View()
	target := tagTarget(run)
	if target == "" {
		return fmt.Errorf("%w: no commit to tag", ports.ErrInvalid)
	}
	if err := requireOnMain(ctx, rc, target); err != nil {
		return err
	}
	force, err := mayMoveTag(ctx, rc, run, target)
	if err != nil {
		return err
	}
	ref, err := rc.Deps.GitHub.UpsertTag(ctx, ports.TagSpec{
		Name: run.Version, Commit: target, Message: "Release " + string(run.Version), Force: force,
	})
	if errors.Is(err, ports.ErrConflict) {
		return fail(deploy.FailGitHub, "tag %s already points at another commit; a release never moves a tag, a hotfix does", run.Version)
	}
	if err != nil {
		return err
	}
	return rc.SetOutputs(ctx, func(o *deploy.Outputs) { o.TagSHA = ref.CommitSHA })
}

// requireOnMain repeats the start-time check right before the tag is
// written, so a resumed run cannot tag a commit force-pushed off main since.
func requireOnMain(ctx context.Context, rc *stage.RunCtx, target deploy.SHA) error {
	ok, err := OnMain(ctx, rc.Deps.GitHub, rc.Deps.Config.MainBranch, target)
	if err != nil || ok {
		return err
	}
	return fail(deploy.FailGitHub, "refusing to tag %s: it is not on %s", short(target), rc.Deps.Config.MainBranch)
}

// mayMoveTag reports whether the run may force-move an existing tag: only a
// hotfix, and only forward. A tag moved to an older or diverged commit would
// rebuild a version with less in it than the one already released.
func mayMoveTag(ctx context.Context, rc *stage.RunCtx, run deploy.Run, target deploy.SHA) (bool, error) {
	if run.Kind != deploy.KindHotfix {
		return false, nil
	}
	ref, found, err := rc.Deps.GitHub.Tag(ctx, run.Version)
	if err != nil || !found {
		return true, err
	}
	cmp, err := rc.Deps.GitHub.Compare(ctx, ports.Ref(ref.CommitSHA), ports.Ref(target))
	if err != nil {
		return false, err
	}
	if cmp.BehindBy > 0 {
		return false, fail(deploy.FailGitHub, "hotfix target %s is not ahead of %s at %s", short(target), run.Version, short(ref.CommitSHA))
	}
	return true, nil
}

// release: the GitHub release for the tag, marked Latest, pointed at the
// tag commit (a hotfix re-points it).

func releaseDone(ctx context.Context, rc *stage.RunCtx) (bool, error) {
	run := rc.View()
	rel, found, err := rc.Deps.GitHub.Release(ctx, run.Version)
	if err != nil || !found {
		return false, err
	}
	if !rel.Latest || rel.TargetCommitish != string(run.Outputs.TagSHA) {
		return false, nil
	}
	return true, recordRelease(ctx, rc, rel)
}

func runRelease(ctx context.Context, rc *stage.RunCtx) error {
	run := rc.View()
	notes, err := releaseNotes(ctx, rc, run)
	if err != nil {
		return err
	}
	rel, err := rc.Deps.GitHub.UpsertRelease(ctx, ports.ReleaseSpec{
		Tag: run.Version, Title: string(run.Version), Notes: notes, Target: run.Outputs.TagSHA,
	})
	if err != nil {
		return err
	}
	return recordRelease(ctx, rc, rel)
}

func recordRelease(ctx context.Context, rc *stage.RunCtx, rel ports.Release) error {
	rc.AddLink(ctx, deploy.Link{Label: "Release " + string(rel.Tag), URL: rel.URL})
	return rc.SetOutputs(ctx, func(o *deploy.Outputs) { o.ReleaseURL = rel.URL })
}

// releaseNotes are read from the changelog at the tag commit, not from the
// request: a hotfix without a refresh and a hand-committed release changelog
// have no entry in the run, and the tag is what the release describes.
func releaseNotes(ctx context.Context, rc *stage.RunCtx, run deploy.Run) (string, error) {
	p := changelogPath(rc.Deps.Config, run.Version)
	f, err := readChangelog(ctx, rc.Deps.GitHub, p, ports.Ref(run.Outputs.TagSHA))
	if err != nil {
		return "", err
	}
	if f == nil {
		return "", fmt.Errorf("%w: %s missing at the tag commit", ports.ErrInvalid, p)
	}
	return f.notes(), nil
}

// notes renders the English highlights the way the hand-made releases
// (v0.2.1-beta, v0.2.2-beta) did: one heading, one bullet per paragraph.
func (f changelogFile) notes() string {
	var b strings.Builder
	b.WriteString("## Highlights\n")
	for _, h := range f.Highlights["en"] {
		b.WriteString("\n- " + h + "\n")
	}
	return b.String()
}
