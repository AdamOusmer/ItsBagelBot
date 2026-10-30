// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package train

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"time"

	"github.com/google/uuid"

	"ItsBagelBot/app/release/internal/ports"
	"ItsBagelBot/app/release/internal/stages/github"
	"ItsBagelBot/internal/domain/rpc/deploy"
)

var (
	versionPattern = regexp.MustCompile(`^v\d+\.\d+\.\d+-beta$`)
	shaPattern     = regexp.MustCompile(`^[0-9a-f]{40}$`)
)

type need int

const (
	never need = iota
	may
	must
)

func (n need) check(name string, have bool) error {
	switch {
	case n == must && !have:
		return fmt.Errorf("%s is required: %w", name, ports.ErrInvalid)
	case n == never && have:
		return fmt.Errorf("%s does not apply to this kind of run: %w", name, ports.ErrInvalid)
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

func Seed(ctx context.Context, gh ports.GitHub, actor deploy.Actor, req deploy.StartRequest) (deploy.Run, error) {
	if err := validate(ctx, gh, req); err != nil {
		return deploy.Run{}, err
	}
	id, err := uuid.NewV7()
	if err != nil {
		return deploy.Run{}, err
	}
	now := time.Now().UTC()
	return deploy.Run{
		ID: deploy.RunID(id.String()), Kind: req.Kind, Version: req.Version, TargetSHA: req.TargetSHA, PRs: req.PRs,
		Changelog: req.Changelog, RollbackTo: req.RollbackTo, Services: req.Services,
		Actor: actor, State: deploy.RunRunning, CreatedAt: now, UpdatedAt: now,
	}, nil
}

func validate(ctx context.Context, gh ports.GitHub, req deploy.StartRequest) error {
	f, ok := accepts[req.Kind]
	if !ok {
		return fmt.Errorf("unknown run kind %q: %w", req.Kind, ports.ErrInvalid)
	}
	err := errors.Join(
		f.version.check("version", req.Version != ""),
		f.rollbackTo.check("rollback_to", req.RollbackTo != ""),
		f.prs.check("prs", len(req.PRs) > 0),
		f.changelog.check("changelog", req.Changelog != nil),
		f.services.check("services", len(req.Services) > 0),
		versionFormat("version", req.Version),
		versionFormat("rollback_to", req.RollbackTo),
	)
	if err == nil && req.Changelog != nil {
		err = github.ValidateChangelog(req.Changelog)
	}
	if err != nil {
		return err
	}
	if err := targetOnMain(ctx, gh, req.TargetSHA); err != nil {
		return err
	}
	return versionState(ctx, gh, req)
}

func targetOnMain(ctx context.Context, gh ports.GitHub, sha deploy.SHA) error {
	if sha == "" {
		return nil
	}
	if !shaPattern.MatchString(string(sha)) {
		return fmt.Errorf("target_sha %q is not a full commit sha: %w", sha, ports.ErrInvalid)
	}
	ok, err := github.OnMain(ctx, gh, "main", sha)
	if err != nil || ok {
		return err
	}
	return fmt.Errorf("target_sha %s is not on main: only reviewed, merged commits deploy: %w", sha, ports.ErrInvalid)
}

func versionFormat(name string, v deploy.Version) error {
	if v == "" || versionPattern.MatchString(string(v)) {
		return nil
	}
	return fmt.Errorf("%s %q is not vX.Y.Z-beta: %w", name, v, ports.ErrInvalid)
}

func versionState(ctx context.Context, gh ports.GitHub, req deploy.StartRequest) error {
	switch req.Kind {
	case deploy.KindRelease, deploy.KindHotfix:
		_, exists, err := gh.Tag(ctx, req.Version)
		if err != nil || exists == (req.Kind == deploy.KindHotfix) {
			return err
		}
		if exists {
			return fmt.Errorf("tag %s already exists: re-run its workflow, or start a hotfix: %w", req.Version, ports.ErrInvalid)
		}
		return fmt.Errorf("tag %s does not exist, a hotfix moves an existing release tag: %w", req.Version, ports.ErrInvalid)
	case deploy.KindRollback:
		_, ok, err := gh.Release(ctx, req.RollbackTo)
		if err != nil || ok {
			return err
		}
		return fmt.Errorf("no release %s to roll back to: %w", req.RollbackTo, ports.ErrInvalid)
	}
	return nil
}
