// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package modules

import (
	"context"
	"testing"

	"ItsBagelBot/app/twitch/sesame/engine"
	"ItsBagelBot/app/twitch/sesame/module"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The moderation module registers !nuke at moderator tier with a shared
// cooldown window, so a stray viewer invocation never reaches the sweep.
func TestModerationRegistersNukeAtModTier(t *testing.T) {
	m := Moderation(engine.Deps{})
	assert.Equal(t, "moderation", m.Name)
	require.Len(t, m.Commands, 1)

	cmd := m.Commands[0]
	assert.Equal(t, "nuke", cmd.Name)
	assert.Equal(t, module.RoleModerator, cmd.Perm)
	assert.Positive(t, cmd.Cooldown)
}

// A nil Deps.Nuke leaves the command inert: it runs, emits nothing, errors
// nothing — the graceful degradation every store-backed module follows.
func TestModerationInertWithoutService(t *testing.T) {
	m := Moderation(engine.Deps{})
	run := m.Commands[0].Run

	emitted := 0
	err := run(context.Background(), &module.Context{}, "free nitro now everyone",
		func(o *module.Output) { emitted++ })
	assert.NoError(t, err, "inert must be silent, not an error")
	assert.Zero(t, emitted)
}
