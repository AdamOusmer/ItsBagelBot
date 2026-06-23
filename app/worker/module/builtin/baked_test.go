package builtin

import (
	"context"
	"testing"

	"ItsBagelBot/app/worker/module"
	"ItsBagelBot/internal/domain/event/lane"
	"ItsBagelBot/internal/domain/outgress"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func bakedCtx(chatterID, text string) *module.Context {
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

func newBaked(special *module.SpecialSet, live module.LiveStore, greet module.GreetStore) *BakedModule {
	return NewBakedModule(special, live, greet, zap.NewNop())
}

// findCmd returns the baked command with the given name.
func findCmd(t *testing.T, m *BakedModule, name string) module.Command {
	t.Helper()
	for _, c := range m.Commands() {
		if c.Name == name {
			return c
		}
	}
	t.Fatalf("baked command %q not declared", name)
	return module.Command{}
}

func TestBakedPing(t *testing.T) {
	m := newBaked(module.NewSpecialSet(""), &fakeLive{}, &fakeGreet{})
	cmd := findCmd(t, m, "ping")
	assert.Equal(t, module.RoleEveryone, cmd.Perm)

	var col collector
	require.NoError(t, cmd.Run(context.Background(), bakedCtx("9", "!ping"), "", col.emit))
	require.Len(t, col.out, 1)
	o := col.out[0]
	assert.Equal(t, outgress.TypeChat, o.Type)
	assert.Equal(t, "2", o.BroadcasterID)
	assert.Contains(t, o.Text, "up for")
}

func TestBakedInfoCommands(t *testing.T) {
	m := newBaked(module.NewSpecialSet(""), &fakeLive{}, &fakeGreet{})
	for name, want := range map[string]string{
		"itsbagelbot": "https://itsbagelbot.dev",
		"source":      "https://github.com/AdamOusmer/ItsBagelBot",
	} {
		cmd := findCmd(t, m, name)
		assert.Equal(t, module.RoleEveryone, cmd.Perm, name)
		var col collector
		require.NoError(t, cmd.Run(context.Background(), bakedCtx("9", "!"+name), "", col.emit))
		require.Len(t, col.out, 1, name)
		assert.Equal(t, outgress.TypeChat, col.out[0].Type, name)
		assert.Contains(t, col.out[0].Text, want, name)
	}
}

func TestBakedAnnounceRequiresModerator(t *testing.T) {
	m := newBaked(module.NewSpecialSet(""), &fakeLive{}, &fakeGreet{})
	// Color table: name -> expected announce color.
	colors := map[string]string{
		"announce":       "primary",
		"announceblue":   "blue",
		"announcegreen":  "green",
		"announceorange": "orange",
		"announcepurple": "purple",
	}
	for name, color := range colors {
		cmd := findCmd(t, m, name)
		// Gate is enforced by the router; here we assert the declared Perm.
		assert.Equal(t, module.RoleModerator, cmd.Perm, name)

		var col collector
		require.NoError(t, cmd.Run(context.Background(), bakedCtx("9", "!"+name+" hi chat"), "hi chat", col.emit))
		require.Len(t, col.out, 1, name)
		o := col.out[0]
		assert.Equal(t, outgress.TypeAnnounce, o.Type, name)
		assert.Equal(t, "2", o.BroadcasterID, name)
		assert.Equal(t, "hi chat", o.Text, name)
		assert.Equal(t, color, o.Color, name)
	}
}

func TestBakedAnnounceEmptyArgsNoOp(t *testing.T) {
	m := newBaked(module.NewSpecialSet(""), &fakeLive{}, &fakeGreet{})
	cmd := findCmd(t, m, "announce")
	var col collector
	require.NoError(t, cmd.Run(context.Background(), bakedCtx("9", "!announce"), "", col.emit))
	assert.Empty(t, col.out)
}

func TestBagelGreetsSpecialFirstMessageWhenLive(t *testing.T) {
	m := newBaked(module.NewSpecialSet("1"), &fakeLive{live: true}, &fakeGreet{first: true})
	var col collector
	require.NoError(t, m.Handle(context.Background(), bakedCtx("1", "hello, not a command"), col.emit))
	require.Len(t, col.out, bagelCount)
	for _, o := range col.out {
		assert.Equal(t, outgress.TypeChat, o.Type)
		assert.Equal(t, "2", o.BroadcasterID)
		assert.Equal(t, bagelMessage, o.Text)
	}
}

func TestBagelIgnoresNonSpecial(t *testing.T) {
	m := newBaked(module.NewSpecialSet("999"), &fakeLive{live: true}, &fakeGreet{first: true})
	var col collector
	require.NoError(t, m.Handle(context.Background(), bakedCtx("1", "hi"), col.emit))
	assert.Empty(t, col.out)
}

func TestBagelIgnoresWhenOffline(t *testing.T) {
	greet := &fakeGreet{first: true}
	m := newBaked(module.NewSpecialSet("1"), &fakeLive{live: false}, greet)
	var col collector
	require.NoError(t, m.Handle(context.Background(), bakedCtx("1", "hi"), col.emit))
	assert.Empty(t, col.out)
	assert.Empty(t, greet.greeted) // must not consume the first-greet slot
}

func TestBagelOncePerStream(t *testing.T) {
	m := newBaked(module.NewSpecialSet("1"), &fakeLive{live: true}, &fakeGreet{first: false})
	var col collector
	require.NoError(t, m.Handle(context.Background(), bakedCtx("1", "hi"), col.emit))
	assert.Empty(t, col.out)
}
