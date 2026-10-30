// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"strconv"
	"strings"

	"go.uber.org/zap"

	"ItsBagelBot/app/release/internal/config"
	"ItsBagelBot/app/release/internal/gh"
	"ItsBagelBot/app/release/internal/ports"
	"ItsBagelBot/app/release/internal/registry"
	"ItsBagelBot/app/release/internal/stage"
	"ItsBagelBot/app/release/internal/stages/github"
	"ItsBagelBot/app/release/internal/train"
	"ItsBagelBot/internal/domain/rpc/deploy"
	"ItsBagelBot/pkg/codec"
	"ItsBagelBot/pkg/svcboot"
)

type flags struct {
	kind, version, rollbackTo, targetSHA, prs, services, changelog, state, actor, stage string
}

func main() {
	var f flags
	flag.StringVar(&f.kind, "kind", "", "release, hotfix, bump, rollback or reapply")
	flag.StringVar(&f.version, "version", "", "vX.Y.Z-beta for release and hotfix")
	flag.StringVar(&f.rollbackTo, "rollback-to", "", "release to roll back to")
	flag.StringVar(&f.targetSHA, "target-sha", "", "main commit to ship; empty ships main's head")
	flag.StringVar(&f.prs, "prs", "", "comma-separated PR numbers to merge first")
	flag.StringVar(&f.services, "services", "", "comma-separated services for a bump")
	flag.StringVar(&f.changelog, "changelog", "", "path to a changelog entry JSON")
	flag.StringVar(&f.state, "state", "run.json", "run state file; an existing one is resumed")
	flag.StringVar(&f.actor, "actor", "", "who started the run")
	flag.StringVar(&f.stage, "stage", "", "run only this stage; empty runs every unfinished stage")
	flag.Parse()

	core, done := svcboot.NewCore("release")
	err := run(core, f)
	done()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		var fail *ports.Fail
		if errors.As(err, &fail) {
			fmt.Fprintln(os.Stderr, strings.Join(fail.LogTail, "\n"))
		}
		os.Exit(1)
	}
}

func run(core svcboot.Core, f flags) error {
	ctx := core.Ctx
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	deps, err := stageDeps(cfg, core.Log)
	if err != nil {
		return err
	}
	seed, err := seedRun(ctx, deps.GitHub, f)
	if err != nil {
		return err
	}
	file, err := train.Open(f.state, seed)
	if err != nil {
		return err
	}
	return train.New(deps, github.All()).Drive(ctx, file, deploy.StageID(f.stage))
}

func seedRun(ctx context.Context, hub ports.GitHub, f flags) (deploy.Run, error) {
	if _, err := os.Stat(f.state); err == nil {
		return deploy.Run{}, nil
	}
	req := deploy.StartRequest{
		Kind: deploy.RunKind(f.kind), Version: deploy.Version(f.version),
		RollbackTo: deploy.Version(f.rollbackTo), TargetSHA: deploy.SHA(f.targetSHA), Services: list(f.services),
	}
	prs, err := numbers(f.prs)
	if err != nil {
		return deploy.Run{}, err
	}
	req.PRs = prs
	if req.Changelog, err = changelog(f.changelog); err != nil {
		return deploy.Run{}, err
	}
	return train.Seed(ctx, hub, deploy.Actor{Login: f.actor}, req)
}

func stageDeps(cfg *config.Config, log *zap.Logger) (stage.Deps, error) {
	hub, err := gh.New(gh.Config{
		AppID: cfg.GitHubAppID, InstallationID: cfg.GitHubInstallationID,
		PrivateKey: cfg.GitHubPrivateKey, Deploy: cfg.Deploy,
	})
	if err != nil {
		return stage.Deps{}, err
	}
	return stage.Deps{
		GitHub:   hub,
		Registry: registry.New(registry.Config{Repo: cfg.Deploy.ImageRepo, Username: cfg.GHCRUsername, Password: cfg.GHCRToken}),
		Clock:    ports.SystemClock{},
		Config:   cfg.Deploy,
		Log:      log,
	}, nil
}

func list(csv string) []string {
	if csv == "" {
		return nil
	}
	return strings.Split(csv, ",")
}

func numbers(csv string) ([]int, error) {
	var out []int
	for _, s := range list(csv) {
		n, err := strconv.Atoi(strings.TrimSpace(strings.TrimPrefix(s, "#")))
		if err != nil {
			return nil, fmt.Errorf("pr %q is not a number", s)
		}
		out = append(out, n)
	}
	return out, nil
}

func changelog(path string) (*deploy.ChangelogEntry, error) {
	if path == "" {
		return nil, nil
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var entry deploy.ChangelogEntry
	return &entry, codec.Unmarshal(b, &entry)
}
