// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package config

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"ItsBagelBot/app/deployer/internal/ports"
)

func TestLoadRequiresGitHubAndGHCRCredentials(t *testing.T) {
	complete := map[string]string{
		"GITHUB_APP_ID":              "12345",
		"GITHUB_APP_INSTALLATION_ID": "67890",
		"GITHUB_APP_PRIVATE_KEY":     "-----BEGIN RSA PRIVATE KEY-----",
		"GHCR_USERNAME":              "bagel-pull",
		"GHCR_TOKEN":                 "token",
	}
	cases := map[string]struct {
		override map[string]string
		wantErr  string
	}{
		"all present": {nil, ""},
		"unparseable app id and empty token": {
			map[string]string{"GITHUB_APP_ID": "abc", "GHCR_TOKEN": ""},
			"missing required env: GITHUB_APP_ID, GHCR_TOKEN",
		},
		"everything missing": {
			map[string]string{
				"GITHUB_APP_ID": "", "GITHUB_APP_INSTALLATION_ID": "", "GITHUB_APP_PRIVATE_KEY": "",
				"GHCR_USERNAME": "", "GHCR_TOKEN": "",
			},
			"missing required env: GITHUB_APP_ID, GITHUB_APP_INSTALLATION_ID, GITHUB_APP_PRIVATE_KEY, GHCR_USERNAME, GHCR_TOKEN",
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			for k, v := range complete {
				t.Setenv(k, v)
			}
			for k, v := range tc.override {
				t.Setenv(k, v)
			}
			_, err := Load()
			got := ""
			if err != nil {
				got = err.Error()
			}
			assert.Equal(t, tc.wantErr, got)
		})
	}
}

func requiredEnv(t *testing.T) {
	t.Helper()
	for k, v := range map[string]string{
		"GITHUB_APP_ID": "1", "GITHUB_APP_INSTALLATION_ID": "1", "GITHUB_APP_PRIVATE_KEY": "key",
		"GHCR_USERNAME": "user", "GHCR_TOKEN": "token",
	} {
		t.Setenv(k, v)
	}
}

func TestLoadDefaultsToConfigModeWithNoJWTVarsSet(t *testing.T) {
	requiredEnv(t)
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	assert.Equal(t, ports.NATSAuthConfig, cfg.Deploy.NATSAuthMode)
	assert.Equal(t, "", cfg.Deploy.NATSSigningSeed)
	assert.Equal(t, "", cfg.Deploy.NATSSysJWT)
	assert.Equal(t, "", cfg.Deploy.NATSSysNKeySeed)
	assert.Equal(t, "", cfg.Deploy.NATSHubURL)
	assert.Equal(t, "", cfg.Deploy.NATSLeafURL)
	assert.Equal(t, 5*time.Minute, cfg.Deploy.ACLReconcileEvery)
	assert.Equal(t, ports.FilePath("deploy/messaging/accounts.yaml"), cfg.Deploy.AccountsFile)
	assert.Equal(t, ports.FilePath("deploy/messaging/accounts.keys.yaml"), cfg.Deploy.AccountsKeysFile)
}

func TestLoadReadsJWTModeVars(t *testing.T) {
	requiredEnv(t)
	t.Setenv("DEPLOY_NATS_AUTH", "jwt")
	t.Setenv("DEPLOY_NATS_SIGNING_SEED", "SOSEED")
	t.Setenv("DEPLOY_NATS_SYS_JWT", "eyJhbGciOi")
	t.Setenv("DEPLOY_NATS_SYS_NKEY_SEED", "SUSEED")
	t.Setenv("DEPLOY_NATS_HUB_URL", "tls://nats.messaging:4222")
	t.Setenv("DEPLOY_NATS_LEAF_URL", "tls://nats-leaf.messaging:4222")
	t.Setenv("DEPLOY_ACL_RECONCILE_EVERY", "90s")

	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	assert.Equal(t, ports.NATSAuthJWT, cfg.Deploy.NATSAuthMode)
	assert.Equal(t, "SOSEED", cfg.Deploy.NATSSigningSeed)
	assert.Equal(t, "eyJhbGciOi", cfg.Deploy.NATSSysJWT)
	assert.Equal(t, "SUSEED", cfg.Deploy.NATSSysNKeySeed)
	assert.Equal(t, "tls://nats.messaging:4222", cfg.Deploy.NATSHubURL)
	assert.Equal(t, "tls://nats-leaf.messaging:4222", cfg.Deploy.NATSLeafURL)
	assert.Equal(t, 90*time.Second, cfg.Deploy.ACLReconcileEvery)
}
