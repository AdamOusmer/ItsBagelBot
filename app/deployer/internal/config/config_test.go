// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package config_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"ItsBagelBot/app/deployer/internal/config"
	"ItsBagelBot/app/deployer/internal/ports"
)

var requiredEnv = map[string]string{
	"GITHUB_APP_ID":              "12345",
	"GITHUB_APP_INSTALLATION_ID": "67890",
	"GITHUB_APP_PRIVATE_KEY":     "-----BEGIN RSA PRIVATE KEY-----",
	"GHCR_USERNAME":              "bagel-pull",
	"GHCR_TOKEN":                 "token",
}

func setEnv(t *testing.T, vars ...map[string]string) {
	t.Helper()
	for _, set := range vars {
		for k, v := range set {
			t.Setenv(k, v)
		}
	}
}

func TestLoadRequiresGitHubAndGHCRCredentials(t *testing.T) {
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
			setEnv(t, requiredEnv, tc.override)
			_, err := config.Load()
			if tc.wantErr == "" {
				require.NoError(t, err)
				return
			}
			assert.EqualError(t, err, tc.wantErr)
		})
	}
}

func TestLoadNATSAuth(t *testing.T) {
	cases := map[string]struct {
		env  map[string]string
		want ports.Config
	}{
		"defaults to config mode with no JWT vars set": {
			nil,
			ports.Config{NATSAuthMode: ports.NATSAuthConfig},
		},
		"reads the JWT mode vars": {
			map[string]string{
				"DEPLOY_NATS_AUTH":          "jwt",
				"DEPLOY_NATS_SIGNING_SEED":  "SOSEED",
				"DEPLOY_NATS_SYS_JWT":       "eyJhbGciOi",
				"DEPLOY_NATS_SYS_NKEY_SEED": "SUSEED",
				"DEPLOY_NATS_HUB_URL":       "tls://nats.messaging:4222",
				"DEPLOY_NATS_LEAF_URL":      "tls://nats-leaf.messaging:4222",
			},
			ports.Config{
				NATSAuthMode:    ports.NATSAuthJWT,
				NATSSigningSeed: "SOSEED",
				NATSSysJWT:      "eyJhbGciOi",
				NATSSysNKeySeed: "SUSEED",
				NATSHubURL:      "tls://nats.messaging:4222",
				NATSLeafURL:     "tls://nats-leaf.messaging:4222",
			},
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			setEnv(t, requiredEnv, tc.env)
			cfg, err := config.Load()
			require.NoError(t, err)

			got := ports.Config{
				NATSAuthMode:    cfg.Deploy.NATSAuthMode,
				NATSSigningSeed: cfg.Deploy.NATSSigningSeed,
				NATSSysJWT:      cfg.Deploy.NATSSysJWT,
				NATSSysNKeySeed: cfg.Deploy.NATSSysNKeySeed,
				NATSHubURL:      cfg.Deploy.NATSHubURL,
				NATSLeafURL:     cfg.Deploy.NATSLeafURL,
			}
			assert.Equal(t, tc.want, got)
		})
	}
}
