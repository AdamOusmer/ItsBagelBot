package modules

import (
	"testing"

	"ItsBagelBot/app/sesame/engine"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// TestAllBuildsAndIndexes wires every shipped module through the registry, which
// exercises Build validation and the first-wins de-dup for all of them at once —
// the closest thing to what main does at startup.
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

	// A representative reserved command is indexed.
	_, ok := reg.Command("ping")
	assert.True(t, ok)

	// Chat and raid event handlers are registered.
	assert.NotEmpty(t, reg.For("channel.chat.message"))
	assert.NotEmpty(t, reg.For("channel.raid"))
	assert.NotEmpty(t, reg.For("stream.online"))
}
