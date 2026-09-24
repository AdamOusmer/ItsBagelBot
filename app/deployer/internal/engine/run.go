// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"context"
	"errors"
	"maps"
	"regexp"
	"slices"
	"time"

	"github.com/google/uuid"

	"ItsBagelBot/app/deployer/internal/ports"
	"ItsBagelBot/app/deployer/internal/stages/github"
	"ItsBagelBot/internal/domain/rpc/deploy"
	"ItsBagelBot/pkg/codec"
)

var (
	versionPattern = regexp.MustCompile(`^v(\d+)\.(\d+)\.(\d+)-beta$`)
	shaPattern     = regexp.MustCompile(`^[0-9a-f]{40}$`)
)

type need int

const (
	never need = iota
	may
	must
)

type field string

func (n need) check(name field, have bool) error {
	switch {
	case n == must && !have:
		return invalid("%s is required", name)
	case n == never && have:
		return invalid("%s does not apply to this kind of run", name)
	}
	return nil
}

type fields struct{ version, rollbackTo, prs, changelog, services need }

var accepts = map[deploy.RunKind]fields{
	deploy.KindRelease:  {version: must, prs: may, changelog: may},
	deploy.KindHotfix:   {version: must, prs: may, changelog: may},
	deploy.KindBump:     {prs: may, services: may},
	deploy.KindRollback: {rollbackTo: must},
	deploy.KindReapply:  {},
}

func (f fields) check(req deploy.StartRequest) error {
	return errors.Join(
		f.version.check("version", req.Version != ""),
		f.rollbackTo.check("rollback_to", req.RollbackTo != ""),
		f.prs.check("prs", len(req.PRs) > 0),
		f.changelog.check("changelog", req.Changelog != nil),
		f.services.check("services", len(req.Services) > 0),
	)
}

func (e *Engine) newRun(ctx context.Context, actor deploy.Actor, req deploy.StartRequest) (deploy.Run, error) {
	if err := e.validate(ctx, req); err != nil {
		return deploy.Run{}, err
	}
	id, err := uuid.NewV7()
	if err != nil {
		return deploy.Run{}, err
	}
	now := e.now()
	return deploy.Run{
		ID: deploy.RunID(id.String()), Kind: req.Kind, Version: req.Version, TargetSHA: req.TargetSHA,
		PRs: slices.Clone(req.PRs), Changelog: req.Changelog, RollbackTo: req.RollbackTo,
		Services: slices.Clone(req.Services), Actor: actor, State: deploy.RunRunning,
		Stages: pendingStages(req.Kind), CreatedAt: now, UpdatedAt: now,
	}, nil
}

func (e *Engine) validate(ctx context.Context, req deploy.StartRequest) error {
	f, ok := accepts[req.Kind]
	if !ok {
		return invalid("unknown run kind %q", req.Kind)
	}
	err := errors.Join(
		f.check(req),
		versionFormat("version", req.Version),
		versionFormat("rollback_to", req.RollbackTo),
		shaFormat(req.TargetSHA),
		distinctPRs(req.PRs),
		e.knownServices(req.Services),
		changelogShape(req.Changelog),
	)
	if err != nil {
		return err
	}
	if err := e.targetOnMain(ctx, req.TargetSHA); err != nil {
		return err
	}
	return e.versionState(ctx, req)
}

func (e *Engine) targetOnMain(ctx context.Context, sha deploy.SHA) error {
	if sha == "" {
		return nil
	}
	main := e.d.Stage.Config.MainBranch
	ok, err := github.OnMain(ctx, e.d.Stage.GitHub, main, sha)
	switch {
	case errors.Is(err, ports.ErrNotFound):
		return invalid("target_sha %s is not a commit of this repository", sha)
	case err != nil:
		return err
	case !ok:
		return invalid("target_sha %s is not on %s: only reviewed, merged commits deploy", sha, main)
	}
	return nil
}

func versionFormat(name field, v deploy.Version) error {
	if v == "" || versionPattern.MatchString(string(v)) {
		return nil
	}
	return invalid("%s %q is not vX.Y.Z-beta", name, v)
}

func shaFormat(sha deploy.SHA) error {
	if sha == "" || shaPattern.MatchString(string(sha)) {
		return nil
	}
	return invalid("target_sha %q is not a full commit sha", sha)
}

func distinctPRs(prs []int) error {
	seen := make(map[int]bool, len(prs))
	for _, n := range prs {
		if n <= 0 || seen[n] {
			return invalid("prs must be distinct positive numbers, got %v", prs)
		}
		seen[n] = true
	}
	return nil
}

func (e *Engine) knownServices(services []string) error {
	for _, s := range services {
		if !slices.Contains(e.d.Services, s) {
			return invalid("unknown service %q", s)
		}
	}
	return nil
}

func changelogShape(entry *deploy.ChangelogEntry) error {
	if entry == nil {
		return nil
	}
	return github.ValidateChangelog(entry)
}

func (e *Engine) versionState(ctx context.Context, req deploy.StartRequest) error {
	switch req.Kind {
	case deploy.KindRelease:
		return e.tagState(ctx, req.Version, false)
	case deploy.KindHotfix:
		return e.tagState(ctx, req.Version, true)
	case deploy.KindRollback:
		return e.releaseExists(ctx, req.RollbackTo)
	}
	return nil
}

func (e *Engine) tagState(ctx context.Context, v deploy.Version, want bool) error {
	_, exists, err := e.d.Stage.GitHub.Tag(ctx, v)
	switch {
	case err != nil:
		return err
	case exists == want:
		return nil
	case want:
		return invalid("tag %s does not exist, a hotfix moves an existing release tag", v)
	}
	return invalid("tag %s already exists: resume its run, or start a hotfix", v)
}

func (e *Engine) releaseExists(ctx context.Context, v deploy.Version) error {
	_, ok, err := e.d.Stage.GitHub.Release(ctx, v)
	if err != nil || ok {
		return err
	}
	return invalid("no release %s to roll back to", v)
}

func pendingStages(kind deploy.RunKind) []deploy.Stage {
	ids := deploy.StagesFor(kind)
	out := make([]deploy.Stage, len(ids))
	for i, id := range ids {
		out[i] = deploy.Stage{ID: id, State: deploy.StatePending}
	}
	return out
}

// Other slices must stay replace-or-append only, or a View races the stage goroutine.
func cloneRun(r *deploy.Run) deploy.Run {
	c := *r
	c.Stages = slices.Clone(r.Stages)
	c.Outputs.Digests = maps.Clone(r.Outputs.Digests)
	return c
}

func stateOf(r *deploy.Run) []byte {
	c := *r
	c.Seq, c.UpdatedAt = 0, time.Time{}
	c.Stages = make([]deploy.Stage, len(r.Stages))
	for i, s := range r.Stages {
		s.Progress, s.Items, s.Links = deploy.Progress{}, nil, nil
		c.Stages[i] = s
	}
	b, _ := codec.Marshal(&c)
	return b
}

func defaultActions(id deploy.StageID) []deploy.Action {
	actions := []deploy.Action{deploy.ActionResume}
	switch id {
	case deploy.StageBuild:
		actions = append(actions, deploy.ActionRerun)
	case deploy.StageRollout, deploy.StageVerify:
		actions = append(actions, deploy.ActionRollback)
	}
	return append(actions, deploy.ActionCancel)
}
