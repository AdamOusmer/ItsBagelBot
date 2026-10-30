// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package config_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"ItsBagelBot/app/release/internal/config"
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
