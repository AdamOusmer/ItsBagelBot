// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package config

import (
	"fmt"
	"strings"
	"time"

	"ItsBagelBot/app/release/internal/ports"
	"ItsBagelBot/pkg/env"
)

type Config struct {
	Deploy ports.Config

	GitHubAppID          int64
	GitHubInstallationID int64
	GitHubPrivateKey     []byte

	GHCRUsername string
	GHCRToken    string
}

func Load() (*Config, error) {
	cfg := &Config{
		Deploy:               loadDeploy(),
		GitHubAppID:          int64(env.GetInt("GITHUB_APP_ID", 0)),
		GitHubInstallationID: int64(env.GetInt("GITHUB_APP_INSTALLATION_ID", 0)),
		GitHubPrivateKey:     []byte(env.Get("GITHUB_APP_PRIVATE_KEY", "")),
		GHCRUsername:         env.Get("GHCR_USERNAME", ""),
		GHCRToken:            env.Get("GHCR_TOKEN", ""),
	}
	if missing := cfg.missing(); len(missing) > 0 {
		return nil, fmt.Errorf("missing required env: %s", strings.Join(missing, ", "))
	}
	return cfg, nil
}

func (c *Config) missing() []string {
	required := []struct {
		key string
		set bool
	}{
		{"GITHUB_APP_ID", c.GitHubAppID > 0},
		{"GITHUB_APP_INSTALLATION_ID", c.GitHubInstallationID > 0},
		{"GITHUB_APP_PRIVATE_KEY", len(c.GitHubPrivateKey) > 0},
		{"GHCR_USERNAME", c.GHCRUsername != ""},
		{"GHCR_TOKEN", c.GHCRToken != ""},
	}
	var out []string
	for _, r := range required {
		if !r.set {
			out = append(out, r.key)
		}
	}
	return out
}

func loadDeploy() ports.Config {
	return ports.Config{
		Owner:             env.Get("DEPLOY_GITHUB_OWNER", "AdamOusmer"),
		Repo:              env.Get("DEPLOY_GITHUB_REPO", "ItsBagelBot"),
		MainBranch:        ports.Branch(env.Get("DEPLOY_MAIN_BRANCH", "main")),
		DeployBranch:      ports.Branch(env.Get("DEPLOY_BRANCH", "deploy")),
		ImageRepo:         env.Get("DEPLOY_IMAGE_REPO", "ghcr.io/adamousmer/itsbagelbot"),
		Workflow:          ports.Workflow(env.Get("DEPLOY_WORKFLOW", "publish-images.yml")),
		CodeSceneCheck:    env.Get("DEPLOY_CODESCENE_CHECK", "CodeScene Code Health Review (main)"),
		ManifestDir:       "deploy/k8s",
		ChangelogDir:      "web/marketing/src/content/changelog",
		UpdateBranchLimit: env.GetInt("DEPLOY_UPDATE_BRANCH_LIMIT", 3),
		LogTailLines:      env.GetInt("DEPLOY_LOG_TAIL_LINES", 50),
		PollEvery:         env.GetDuration("DEPLOY_POLL_EVERY", 5*time.Second),
	}
}
