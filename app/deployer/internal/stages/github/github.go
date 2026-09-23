// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// Package github holds the GitHub-side stages: merge_prs, changelog, tag,
// release, build, digests and pin_pr, plus the pin line parser and rewriter.
//
// Every stage is a pair of functions wrapped in ghStage: done reads GitHub to
// decide whether the effect already exists and, when it does, records the
// outputs later stages need (a resumed run that finds the changelog already
// merged still has to hand its merge commit to tag), and run does the work.
package github

import (
	"context"
	"errors"
	"path"
	"strings"
	"time"

	"ItsBagelBot/app/deployer/internal/ports"
	"ItsBagelBot/app/deployer/internal/stage"
	"ItsBagelBot/internal/domain/rpc/deploy"
)

// All returns this package's stages in train order.
func All() []stage.Stage {
	return []stage.Stage{
		ghStage{deploy.StageMergePRs, mergePRsDone, runMergePRs},
		ghStage{deploy.StageChangelog, changelogDone, runChangelog},
		ghStage{deploy.StageTag, tagDone, runTag},
		ghStage{deploy.StageRelease, releaseDone, runRelease},
		ghStage{deploy.StageBuild, buildDone, runBuild},
		ghStage{deploy.StageDigests, digestsDone, runDigests},
		ghStage{deploy.StagePinPR, pinsDone, runPins},
	}
}

type ghStage struct {
	id   deploy.StageID
	done func(ctx context.Context, rc *stage.RunCtx) (bool, error)
	run  func(ctx context.Context, rc *stage.RunCtx) error
}

func (s ghStage) ID() deploy.StageID { return s.id }

func (s ghStage) Done(ctx context.Context, rc *stage.RunCtx) (bool, error) {
	return s.done(ctx, rc)
}

func (s ghStage) Run(ctx context.Context, rc *stage.RunCtx) error {
	return githubFailure(s.run(ctx, rc))
}

// passThrough are the errors githubFailure leaves alone: the stage contract
// sentinels, shutdown, and a malformed request (an operator input problem,
// not a GitHub one).
var passThrough = []error{stage.ErrSkipped, stage.ErrCancelled, context.Canceled, context.DeadlineExceeded, ports.ErrInvalid}

// githubFailure turns a bare adapter error into a FailGitHub the operator can
// resume from. Nearly every error these stages see is a GitHub API call
// failing (rate limit, 5xx, a ruleset refusing a merge); recorded as
// FailInternal the console would call it a deployer bug and offer nothing.
func githubFailure(err error) error {
	if err == nil {
		return nil
	}
	if _, ok := ports.AsFail(err); ok {
		return err
	}
	for _, target := range passThrough {
		if errors.Is(err, target) {
			return err
		}
	}
	return fail(deploy.FailGitHub, "%v", err)
}

// fail is a Failure the operator resumes after fixing the cause.
func fail(code deploy.FailureCode, format string, args ...any) *stage.Fail {
	f := stage.Failf(code, format, args...)
	f.Actions = []deploy.Action{deploy.ActionResume, deploy.ActionCancel}
	return f
}

// poll calls step until it reports done or fails, sleeping PollEvery between
// calls. Cancel is honoured between polls: waiting on GitHub is always a safe
// point, nothing is half-written while a check or a build is pending.
func poll(ctx context.Context, rc *stage.RunCtx, step func(context.Context) (bool, error)) error {
	for {
		done, err := step(ctx)
		if err != nil || done {
			return err
		}
		if rc.Cancelled() {
			return stage.ErrCancelled
		}
		if err := sleep(ctx, rc.Deps.Config.PollEvery); err != nil {
			return err
		}
	}
}

func sleep(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// Branch names. A release reuses its fixed name across runs so a second run
// for the same version adopts the first run's open or merged PR instead of
// opening another. A hotfix or rollback of an existing version must not: the
// release's merged PR already owns the fixed name, and FindPR would report
// the refresh as done before it ran, so those carry the run key.

func changelogBranch(run deploy.Run) ports.Branch {
	if run.Kind == deploy.KindHotfix {
		return ports.Branch("chore/changelog-" + string(run.Version) + "-hotfix-" + runKey(run.ID))
	}
	return ports.Branch("chore/changelog-" + string(run.Version))
}

// A bump carries the run key too: keyed on the sha alone, a re-bump at the
// same main head after a rollback adopted the first bump's merged PR, took
// its old merge commit as PinSHA and rolled manifests from that stale
// commit, reverting every manifest change merged since.
func pinBranch(run deploy.Run) ports.Branch {
	switch run.Kind {
	case deploy.KindBump:
		return ports.Branch("chore/deploy-bump-" + short(run.TargetSHA) + "-" + runKey(run.ID))
	case deploy.KindHotfix:
		return ports.Branch("chore/deploy-" + string(run.Version) + "-hotfix-" + runKey(run.ID))
	case deploy.KindRollback:
		return ports.Branch("chore/rollback-" + string(run.RollbackTo) + "-" + runKey(run.ID))
	}
	return ports.Branch("chore/deploy-" + string(run.Version))
}

// runKey is the run id reduced to what a branch name accepts, at most 12
// characters.
func runKey(id deploy.RunID) string {
	key := strings.Map(branchRune, strings.ToLower(string(id)))
	return key[:min(len(key), 12)]
}

func branchRune(r rune) rune {
	if strings.ContainsRune("abcdefghijklmnopqrstuvwxyz0123456789", r) {
		return r
	}
	return -1
}

// short is the 12-character sha publish-images uses in its tags.
func short(sha deploy.SHA) string { return string(sha)[:min(len(sha), 12)] }

func changelogPath(cfg ports.Config, v deploy.Version) ports.FilePath {
	return ports.FilePath(path.Join(string(cfg.ChangelogDir), string(v)+".json"))
}

func mainRef(cfg ports.Config) ports.Ref { return ports.Ref(cfg.MainBranch) }

// tagTarget is the commit the version tag points at: the changelog merge
// commit when this run made one, otherwise the target commit (a hotfix that
// did not refresh the changelog, a release whose changelog was already
// committed by hand).
func tagTarget(run deploy.Run) deploy.SHA {
	if run.Outputs.ChangelogSHA != "" {
		return run.Outputs.ChangelogSHA
	}
	return run.TargetSHA
}

// OnMain reports whether sha is main's head or one of its ancestors
// (main...sha is not ahead). Everything the train tags, builds and pins must
// be: a tag push runs publish-images as it exists at the tagged commit, with
// attestations: write, so a PR head sha pasted by mistake (or a commit from
// the fork network) would ship unreviewed code, workflow included, with
// valid provenance and a matching revision label that the digests stage
// accepts. Only reachability from main proves the ruleset and CodeScene
// gate saw it.
func OnMain(ctx context.Context, gh ports.GitHub, main ports.Branch, sha deploy.SHA) (bool, error) {
	cmp, err := gh.Compare(ctx, ports.Ref(main), ports.Ref(sha))
	if err != nil {
		return false, err
	}
	return cmp.AheadBy == 0, nil
}

func releaseURL(cfg ports.Config, v deploy.Version) string {
	return "https://github.com/" + cfg.Owner + "/" + cfg.Repo + "/releases/tag/" + string(v)
}
