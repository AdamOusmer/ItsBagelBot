// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package modules

import (
	"testing"

	"ItsBagelBot/app/twitch/sesame/engine"
	"ItsBagelBot/app/twitch/sesame/module"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAutomodRegistersASilentChatHandler(t *testing.T) {
	m := Automod(engine.Deps{})
	assert.Equal(t, "automod", m.Name, "the MODULE_CATALOG id")
	assert.Equal(t, module.KindDefault, m.Kind, "ships enabled, toggleable")
	assert.Empty(t, m.Commands)
	h := m.Events["channel.chat.message"]
	require.NotNil(t, h, "the chat handler forces the ModuleView fetch")
	var col collector
	require.NoError(t, h(t.Context(), &module.Context{}, col.emit))
	assert.Empty(t, col.out)
}
