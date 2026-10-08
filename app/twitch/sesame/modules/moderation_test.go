// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package modules

import (
	"testing"

	"ItsBagelBot/app/twitch/sesame/engine"

	"github.com/stretchr/testify/assert"
)

func TestNukeIsSilentWithoutModerationService(t *testing.T) {
	out, err := runChatErr(t, Moderation(engine.Deps{}), chatCtx("42", "mod", "moderator"), "!nuke free nitro now everyone")
	assert.NoError(t, err, "inert must be silent, not an error")
	assert.Empty(t, out)
}
