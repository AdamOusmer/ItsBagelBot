// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
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
