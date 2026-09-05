// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package rpc

import (
	"testing"

	ddiscord "ItsBagelBot/internal/domain/discord"

	"github.com/stretchr/testify/assert"
)

func TestValidateConfigAcceptsUnsetAndWellFormed(t *testing.T) {
	assert.Empty(t, validateConfig(ddiscord.Config{}), "every control unset is a valid config")
	assert.Empty(t, validateConfig(ddiscord.Config{
		GuildID:       "123456789012345678",
		LiveChannelID: "12345678901234567",
		LiveEnabled:   "on",
		VoiceEnabled:  "off",
	}))
}

func TestValidateConfigNamesTheBadFields(t *testing.T) {
	bad := validateConfig(ddiscord.Config{
		// A channel name pasted into an id field: the one mistake the
		// dashboard can actually make.
		LiveChannelID:  "#announcements",
		ModsRoleID:     "12",
		GoodbyeEnabled: "yes",
	})
	assert.ElementsMatch(t, []string{"liveChannelId", "modsRoleId", "goodbyeEnabled"}, bad)
}

func TestValidSnowflakeBounds(t *testing.T) {
	assert.True(t, validSnowflake(""))
	assert.True(t, validSnowflake("12345678901234567"))
	assert.True(t, validSnowflake("12345678901234567890"))
	assert.False(t, validSnowflake("1234567890123456"), "16 digits predates Discord's epoch")
	assert.False(t, validSnowflake("123456789012345678901"), "21 digits cannot be a 64-bit id")
	assert.False(t, validSnowflake("1234567890123456a"))
}
