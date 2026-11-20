// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package validate_test

import (
	"os"
	"testing"

	"ItsBagelBot/internal/domain/validate"
	"ItsBagelBot/pkg/codec"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type twitchNames struct {
	Logins []struct {
		Value    string `json:"value"`
		Login    bool   `json:"login"`
		Username bool   `json:"username"`
	} `json:"logins"`
	DisplayNames []struct {
		Value string `json:"value"`
		Valid bool   `json:"valid"`
	} `json:"display_names"`
}

func TestTwitchNamesFollowTwitchRules(t *testing.T) {
	raw, err := os.ReadFile("testdata/twitch_names.json")
	require.NoError(t, err)
	var names twitchNames
	require.NoError(t, codec.Unmarshal(raw, &names))

	for _, c := range names.Logins {
		got := [2]bool{validate.Login(c.Value) == nil, validate.Username(c.Value) == nil}
		assert.Equal(t, [2]bool{c.Login, c.Username}, got, "login %q", c.Value)
	}
	for _, c := range names.DisplayNames {
		assert.Equal(t, c.Valid, validate.DisplayName(c.Value) == nil, "display name %q", c.Value)
	}
}
