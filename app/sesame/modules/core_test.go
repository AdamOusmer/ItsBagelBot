package modules

import (
	"context"
	"testing"

	"ItsBagelBot/app/sesame/engine"
	"ItsBagelBot/app/sesame/module"
	"ItsBagelBot/internal/domain/event/lane"
	"ItsBagelBot/internal/domain/outgress"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// --- test doubles ---

type fakeLive struct {
	live bool
	err  error
}

func (f *fakeLive) IsLive(context.Context, uint64) (bool, error) { return f.live, f.err }
func (f *fakeLive) SetLive(context.Context, uint64) error        { return nil }
func (f *fakeLive) ClearLive(context.Context, uint64) error      { return nil }

type fakeGreet struct {
	first   bool
	greeted []string
	resetN  int
}

func (f *fakeGreet) FirstGreet(_ context.Context, _ uint64, id string) (bool, error) {
	f.greeted = append(f.greeted, id)
	return f.first, nil
}
func (f *fakeGreet) ResetGreets(context.Context, uint64) error { f.resetN++; return nil }

type collector struct{ out []module.Output }

func (c *collector) emit(o *module.Output) { c.out = append(c.out, *o) }

func coreDeps(special *engine.SpecialSet, live engine.LiveStore, greet engine.GreetStore) engine.Deps {
	return engine.Deps{Special: special, Live: live, Greet: greet, Log: zap.NewNop()}
}

func findCmd(t *testing.T, m module.Module, name string) module.Command {
	t.Helper()
	for _, c := range m.Commands {
		if c.Name == name {
			return c
		}
	}
	t.Fatalf("command %q not declared", name)
	return module.Command{}
}

func coreCtx(chatterID, text string) *module.Context {
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

// --- commands ---

func TestCorePing(t *testing.T) {
	m := Core(coreDeps(engine.NewSpecialSet(""), &fakeLive{}, &fakeGreet{}))
	cmd := findCmd(t, m, "ping")
	assert.Equal(t, module.RoleEveryone, cmd.Perm)

	var col collector
	require.NoError(t, cmd.Run(context.Background(), coreCtx("9", "!ping"), "", col.emit))
	require.Len(t, col.out, 1)
	assert.Equal(t, outgress.TypeChat, col.out[0].Type)
	assert.Equal(t, "2", col.out[0].BroadcasterID)
	assert.Contains(t, col.out[0].Text, "up for")
}

func TestCoreInfoCommands(t *testing.T) {
	m := Core(coreDeps(engine.NewSpecialSet(""), &fakeLive{}, &fakeGreet{}))
	for name, want := range map[string]string{
		"itsbagelbot": "https://itsbagelbot.com",
		"source":      "https://github.com/AdamOusmer/ItsBagelBot",
	} {
		cmd := findCmd(t, m, name)
		assert.Equal(t, module.RoleEveryone, cmd.Perm, name)
		var col collector
		require.NoError(t, cmd.Run(context.Background(), coreCtx("9", "!"+name), "", col.emit))
		require.Len(t, col.out, 1, name)
		assert.Equal(t, outgress.TypeChat, col.out[0].Type, name)
		assert.Contains(t, col.out[0].Text, want, name)
	}
}

// --- bagel greeting (non-command chat path) ---

func bagelHandler(t *testing.T, m module.Module) module.EventHandler {
	t.Helper()
	h := m.Events["channel.chat.message"]
	require.NotNil(t, h, "core module must handle channel.chat.message")
	return h
}

func TestBagelGreetsSpecialFirstMessageWhenLive(t *testing.T) {
	m := Core(coreDeps(engine.NewSpecialSet("1"), &fakeLive{live: true}, &fakeGreet{first: true}))
	var col collector
	require.NoError(t, bagelHandler(t, m)(context.Background(), coreCtx("1", "hello, not a command"), col.emit))
	require.Len(t, col.out, bagelCount)
	for _, o := range col.out {
		assert.Equal(t, outgress.TypeChat, o.Type)
		assert.Equal(t, "2", o.BroadcasterID)
		assert.Equal(t, bagelMessage, o.Text)
	}
}

func TestBagelIgnoresNonSpecial(t *testing.T) {
	m := Core(coreDeps(engine.NewSpecialSet("999"), &fakeLive{live: true}, &fakeGreet{first: true}))
	var col collector
	require.NoError(t, bagelHandler(t, m)(context.Background(), coreCtx("1", "hi"), col.emit))
	assert.Empty(t, col.out)
}

func TestBagelIgnoresWhenOffline(t *testing.T) {
	greet := &fakeGreet{first: true}
	m := Core(coreDeps(engine.NewSpecialSet("1"), &fakeLive{live: false}, greet))
	var col collector
	require.NoError(t, bagelHandler(t, m)(context.Background(), coreCtx("1", "hi"), col.emit))
	assert.Empty(t, col.out)
	assert.Empty(t, greet.greeted) // must not consume the first-greet slot
}

func TestBagelOncePerStream(t *testing.T) {
	m := Core(coreDeps(engine.NewSpecialSet("1"), &fakeLive{live: true}, &fakeGreet{first: false}))
	var col collector
	require.NoError(t, bagelHandler(t, m)(context.Background(), coreCtx("1", "hi"), col.emit))
	assert.Empty(t, col.out)
}
