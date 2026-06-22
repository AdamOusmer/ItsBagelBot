package builtin

import (
	"context"
	"testing"

	"ItsBagelBot/app/worker/module"
	"ItsBagelBot/internal/domain/event/lane"
	"ItsBagelBot/internal/projection"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func sysCtx(chatterID, text string) *module.Context {
	return &module.Context{
		Env: lane.Envelope{
			Type:              "channel.chat.message",
			BroadcasterUserID: "2",
			ChatterUserID:     chatterID,
			Text:              text,
		},
		Regress:       module.RegressPremium,
		BroadcasterID: 2,
		Log:           zap.NewNop(),
	}
}

func runSystem(t *testing.T, special *module.SpecialSet, live module.LiveStore, greet module.GreetStore, c *module.Context) []*replyView {
	t.Helper()
	m := NewSystemModule(special, live, greet, zap.NewNop())
	out, err := m.Handle(context.Background(), c)
	require.NoError(t, err)
	return decodeReplies(t, out)
}

func TestSystemPing(t *testing.T) {
	replies := runSystem(t, module.NewSpecialSet(""), &fakeLive{}, &fakeGreet{}, sysCtx("9", "!ping"))
	require.Len(t, replies, 1)
	assert.Contains(t, replies[0].Message, "up for")
}

func TestSystemItsbagelbot(t *testing.T) {
	replies := runSystem(t, module.NewSpecialSet(""), &fakeLive{}, &fakeGreet{}, sysCtx("9", "!itsbagelbot"))
	require.Len(t, replies, 1)
	assert.Contains(t, replies[0].Message, websiteURL)
}

func TestSystemIgnoresUnknownCommand(t *testing.T) {
	assert.Empty(t, runSystem(t, module.NewSpecialSet(""), &fakeLive{}, &fakeGreet{}, sysCtx("9", "!whatever")))
}

func TestBagelGreetsSpecialFirstMessageWhenLive(t *testing.T) {
	special := module.NewSpecialSet("1")
	replies := runSystem(t, special, &fakeLive{live: true}, &fakeGreet{first: true}, sysCtx("1", "hello, not a command"))
	require.Len(t, replies, bagelCount)
	for _, r := range replies {
		assert.Equal(t, bagelMessage, r.Message)
	}
}

func TestBagelIgnoresNonSpecial(t *testing.T) {
	assert.Empty(t, runSystem(t, module.NewSpecialSet("999"), &fakeLive{live: true}, &fakeGreet{first: true}, sysCtx("1", "hi")))
}

func TestBagelIgnoresWhenOffline(t *testing.T) {
	greet := &fakeGreet{first: true}
	assert.Empty(t, runSystem(t, module.NewSpecialSet("1"), &fakeLive{live: false}, greet, sysCtx("1", "hi")))
	assert.Empty(t, greet.greeted) // must not consume the first-greet slot
}

func TestBagelOncePerStream(t *testing.T) {
	assert.Empty(t, runSystem(t, module.NewSpecialSet("1"), &fakeLive{live: true}, &fakeGreet{first: false}, sysCtx("1", "hi")))
}

func TestBagelAndSystemCommandFireTogether(t *testing.T) {
	// A special user's first message that is also !ping: 3 bagels + the ping reply.
	special := module.NewSpecialSet("1")
	replies := runSystem(t, special, &fakeLive{live: true}, &fakeGreet{first: true}, sysCtx("1", "!ping"))
	require.Len(t, replies, bagelCount+1)
	assert.Contains(t, replies[bagelCount].Message, "up for")
}

func TestCommandModuleSkipsSystemCommand(t *testing.T) {
	// Even a custom command named "ping" must not be served by CommandModule;
	// the system module owns it.
	r := fakeReader{found: true, command: projection.Command{Name: "ping", Response: "custom", IsActive: true}}
	m := NewCommandModule(r, &fakeLive{}, &fakeCooldown{allow: true}, zap.NewNop())
	out, err := m.Handle(context.Background(), sysCtx("9", "!ping"))
	require.NoError(t, err)
	assert.Empty(t, decodeReplies(t, out))
}
