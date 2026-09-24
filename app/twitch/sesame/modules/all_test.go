// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package modules

import (
	"testing"

	"ItsBagelBot/app/twitch/sesame/engine"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestAllBuildsAndIndexes(t *testing.T) {
	d := engine.Deps{
		Special: engine.NewSpecialSet(""),
		Live:    &fakeLive{},
		Greet:   &fakeGreet{},
		Log:     zap.NewNop(),
	}
	mods := All(d)
	require.NotEmpty(t, mods)

	reg := engine.NewRegistry(zap.NewNop(), mods...)

	_, ok := reg.Command("ping")
	assert.True(t, ok)

	assert.NotEmpty(t, reg.For("channel.chat.message"))
	assert.NotEmpty(t, reg.For("channel.raid"))
	assert.NotEmpty(t, reg.For("stream.online"))
}
