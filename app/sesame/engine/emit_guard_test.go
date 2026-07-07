package engine

import (
	"testing"

	"ItsBagelBot/app/sesame/module"
	"ItsBagelBot/internal/moderation"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The send-time floor guard: the bot must never SAY floor content, no matter
// what a runtime variable injected into a saved-clean template. A module that
// tries to emit a chat line carrying a floor term publishes nothing; a clean
// line still goes out.
func TestEmitGuardSuppressesFloorContent(t *testing.T) {
	slur := moderation.EmbeddedLexicon().Terms(moderation.CatHate)[0]

	pub := &fakePublisher{}
	p := newPipelineWith(pub, fakeReader{},
		emitModule("", module.KindCore, "so true "+slur+" moment"))
	require.NoError(t, p.Process(chatMsg(t, "standard", "hello")))
	assert.Empty(t, pub.got, "a floor-carrying emission must be suppressed")

	pub2 := &fakePublisher{}
	p2 := newPipelineWith(pub2, fakeReader{},
		emitModule("", module.KindCore, "hell of a play, that was bullshit ref"))
	require.NoError(t, p2.Process(chatMsg(t, "standard", "hello")))
	assert.Len(t, pub2.got, 1, "milder language still goes out - only the floor is suppressed")
}
