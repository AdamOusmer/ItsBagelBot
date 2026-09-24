// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package config

import (
	"fmt"
	"math"
	"strings"
	"time"

	"ItsBagelBot/app/deployer/internal/ports"
	"ItsBagelBot/internal/domain/rpc/deploy"
	"ItsBagelBot/pkg/env"
	"ItsBagelBot/pkg/svcboot"
)

type Config struct {
	svcboot.Infra

	Deploy ports.Config

	RPCPrefix        string
	UsersAuthSubject string
	RPCTimeout       time.Duration

	GitHubAppID          int64
	GitHubInstallationID int64
	GitHubPrivateKey     []byte

	GHCRUsername string
	GHCRToken    string
}

func Load() (*Config, error) {
	cfg := &Config{
		Infra:                svcboot.LoadInfra(),
		Deploy:               loadDeploy(),
		RPCPrefix:            env.Get("NATS_DEPLOY_RPC_PREFIX", deploy.Prefix),
		UsersAuthSubject:     env.Get("NATS_ADMIN_AUTH_CHECK_SUBJECT", "bagel.rpc.admin.user.auth.check"),
		RPCTimeout:           env.GetDuration("DEPLOY_RPC_TIMEOUT", 20*time.Second),
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
		Owner:                 env.Get("DEPLOY_GITHUB_OWNER", "AdamOusmer"),
		Repo:                  env.Get("DEPLOY_GITHUB_REPO", "ItsBagelBot"),
		MainBranch:            ports.Branch(env.Get("DEPLOY_MAIN_BRANCH", "main")),
		ImageRepo:             env.Get("DEPLOY_IMAGE_REPO", "ghcr.io/adamousmer/itsbagelbot"),
		Workflow:              ports.Workflow(env.Get("DEPLOY_WORKFLOW", "publish-images.yml")),
		CodeSceneCheck:        env.Get("DEPLOY_CODESCENE_CHECK", "CodeScene Code Health Review (main)"),
		ManifestDir:           "deploy/k8s",
		MessagingDir:          "deploy/messaging",
		PriorityClasses:       "deploy/k8s/priorityclasses.yaml",
		StatusRoutes:          "deploy/db/status-routes.yaml",
		ChangelogDir:          "web/marketing/src/content/changelog",
		RolloutTimeout:        env.GetDuration("DEPLOY_ROLLOUT_TIMEOUT", 8*time.Minute),
		ACLTimeout:            env.GetDuration("DEPLOY_ACL_TIMEOUT", 3*time.Minute),
		FailedSchedulingAfter: env.GetDuration("DEPLOY_FAILED_SCHEDULING_AFTER", 60*time.Second),
		RestartLimit:          int32Env("DEPLOY_RESTART_LIMIT", 3),
		UpdateBranchLimit:     env.GetInt("DEPLOY_UPDATE_BRANCH_LIMIT", 3),
		LogTailLines:          env.GetInt("DEPLOY_LOG_TAIL_LINES", 50),
		PollEvery:             env.GetDuration("DEPLOY_POLL_EVERY", 5*time.Second),
		LockTTL:               env.GetDuration("DEPLOY_LOCK_TTL", 2*time.Minute),
		HeartbeatEvery:        env.GetDuration("DEPLOY_HEARTBEAT_EVERY", 20*time.Second),
	}
}

func int32Env(key string, def int32) int32 {
	n := env.GetInt(key, int(def))
	if n < 0 || n > math.MaxInt32 {
		return def
	}
	return int32(n)
}
