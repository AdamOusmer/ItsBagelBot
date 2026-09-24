// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

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

var passThrough = []error{stage.ErrSkipped, stage.ErrCancelled, context.Canceled, context.DeadlineExceeded, ports.ErrInvalid}

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

func fail(code deploy.FailureCode, format string, args ...any) *stage.Fail {
	f := stage.Failf(code, format, args...)
	f.Actions = []deploy.Action{deploy.ActionResume, deploy.ActionCancel}
	return f
}

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

// Only a release reuses a fixed name; others need the run key or FindPR adopts an older merged PR.
func changelogBranch(run deploy.Run) ports.Branch {
	if run.Kind == deploy.KindHotfix {
		return ports.Branch("chore/changelog-" + string(run.Version) + "-hotfix-" + runKey(run.ID))
	}
	return ports.Branch("chore/changelog-" + string(run.Version))
}

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

func short(sha deploy.SHA) string { return string(sha)[:min(len(sha), 12)] }

func changelogPath(cfg ports.Config, v deploy.Version) ports.FilePath {
	return ports.FilePath(path.Join(string(cfg.ChangelogDir), string(v)+".json"))
}

func mainRef(cfg ports.Config) ports.Ref { return ports.Ref(cfg.MainBranch) }

func tagTarget(run deploy.Run) deploy.SHA {
	if run.Outputs.ChangelogSHA != "" {
		return run.Outputs.ChangelogSHA
	}
	return run.TargetSHA
}

// Everything the train tags must be on main: a tag push gets attestation rights.
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
