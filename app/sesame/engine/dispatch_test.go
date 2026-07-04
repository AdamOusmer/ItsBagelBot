package engine

import (
	"context"
	"testing"

	"ItsBagelBot/app/sesame/module"
	"ItsBagelBot/internal/domain/event/lane"
	"ItsBagelBot/internal/domain/outgress"
	"ItsBagelBot/internal/projection"

	"github.com/bytedance/sonic"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// chatCtx builds a chat Context for the given text and optional badge role.
func chatCtx(text, badgeRole string) *module.Context {
	env := lane.Envelope{
		Type:              chatType,
		Text:              text,
		BroadcasterUserID: "123",
		ChatterUserID:     "999",
		ChatterUserLogin:  "alice",
	}
	if badgeRole != "" {
		env.Badges = []lane.Badge{{SetID: badgeRole}}
	}
	return &module.Context{Env: env, BroadcasterID: 123, Log: zap.NewNop()}
}

// collectDispatch runs the command stage and returns the emitted outputs.
func collectDispatch(p *Pipeline, c *module.Context) []module.Output {
	var got []module.Output
	_ = p.dispatchCommand(context.Background(), c, nil, func(o *module.Output) { got = append(got, *o) })
	return got
}

func chatMessageText(t *testing.T, m outgress.Message) string {
	t.Helper()
	var inner struct {
		Message string `json:"message"`
	}
	require.NoError(t, sonic.Unmarshal(m.Payload, &inner))
	return inner.Message
}

// --- custom (broadcaster-defined) command dispatch ---

func customPipeline(resp, perm string) *Pipeline {
	reg := NewRegistry(zap.NewNop()) // no baked commands
	d := Deps{
		Proj:     fakeReader{cmd: projection.Command{Name: "so", Response: resp, IsActive: true, Perm: perm}, cmdFound: true},
		Live:     liveAlways{},
		Cooldown: NoopCooldown{},
		Pub:      &fakePublisher{},
		Log:      zap.NewNop(),
	}
	return NewPipeline(d, reg, Config{OutgressPremium: premiumSubj, OutgressStandard: standardSubj})
}

func TestCustomAnnounceAllowedForEveryone(t *testing.T) {
	p := customPipeline("/announce {user} says: {args}; target={target}", "everyone")
	got := collectDispatch(p, chatCtx("!so @bob raid incoming", ""))
	require.Len(t, got, 1)
	assert.Equal(t, outgress.TypeAnnounce, got[0].Type)
	assert.Equal(t, "primary", got[0].Color)
	assert.Equal(t, "alice says: @bob raid incoming; target=bob", got[0].Text)
}

func TestCustomAnnounceEmptySkipped(t *testing.T) {
	p := customPipeline("/announce", "everyone")
	assert.Empty(t, collectDispatch(p, chatCtx("!so", "moderator")))
}

func TestCustomPlainChatStillEmits(t *testing.T) {
	p := customPipeline("hello {sender}", "everyone")
	got := collectDispatch(p, chatCtx("!so", ""))
	require.Len(t, got, 1)
	assert.Equal(t, outgress.TypeChat, got[0].Type)
}

// --- baked command dispatch + gate ---

// cmdEmit builds a module owning one everyone-perm command that emits reply.
func cmdEmit(name string, kind module.Kind, trigger, reply string) module.Module {
	b := module.NewModule(name, kind)
	b.Command(trigger).Everyone().Run(func(_ context.Context, c *module.Context, _ string, emit module.Emit) error {
		emit(&module.Output{Type: outgress.TypeChat, BroadcasterID: c.Env.BroadcasterUserID, Text: reply})
		return nil
	})
	return b.Build()
}

func TestBakedCommandRuns(t *testing.T) {
	p := newPipelineWith(&fakePublisher{}, fakeReader{}, cmdEmit("", module.KindCore, "ping", "pong"))
	got := collectDispatch(p, chatCtx("!ping", ""))
	require.Len(t, got, 1)
	assert.Equal(t, "pong", got[0].Text)
}

func TestBakedCommandPermGate(t *testing.T) {
	// A mod-only command: an everyone chatter is gated out.
	b := module.NewModule("", module.KindCore)
	b.Command("clear").Mod().Run(func(_ context.Context, c *module.Context, _ string, emit module.Emit) error {
		emit(&module.Output{Type: outgress.TypeChat, BroadcasterID: c.Env.BroadcasterUserID, Text: "ok"})
		return nil
	})
	p := newPipelineWith(&fakePublisher{}, fakeReader{}, b.Build())

	assert.Empty(t, collectDispatch(p, chatCtx("!clear", "")))             // everyone -> gated
	require.Len(t, collectDispatch(p, chatCtx("!clear", "moderator")), 1) // mod -> runs
}

// TestBakedOutputRoutedByMiddleware proves announce is post-processing, not a
// command: a baked command that writes "/announce hi" is routed to an announce
// action by the shared middleware, exactly as a custom command would be.
func TestBakedOutputRoutedByMiddleware(t *testing.T) {
	b := module.NewModule("", module.KindCore)
	b.Command("hype").Everyone().Run(func(_ context.Context, c *module.Context, _ string, emit module.Emit) error {
		emit(&module.Output{Type: outgress.TypeChat, BroadcasterID: c.Env.BroadcasterUserID, Text: "/announce hi"})
		return nil
	})
	p := newPipelineWith(&fakePublisher{}, fakeReader{}, b.Build())

	got := collectDispatch(p, chatCtx("!hype", ""))
	require.Len(t, got, 1)
	assert.Equal(t, outgress.TypeAnnounce, got[0].Type)
	assert.Equal(t, "primary", got[0].Color)
	assert.Equal(t, "hi", got[0].Text)
}

// TestNamedCoreCommandAlwaysRuns proves a named built-in (KindCore) command runs
// with no ModuleView fetched and cannot be gated off by a missing toggle.
func TestNamedCoreCommandAlwaysRuns(t *testing.T) {
	sys := cmdEmit("system", module.KindCore, "sys", "ok")
	pub := &fakePublisher{}
	p := newPipelineWith(pub, fakeReader{}, sys)
	require.NoError(t, p.Process(chatMsg(t, "standard", "!sys")))
	require.Len(t, pub.got, 1)
	assert.Equal(t, "ok", chatMessageText(t, pub.got[0].msg))
}

// --- integration: a command is gated by its owning module's enable state ---

func TestOptInCommandGatedByModule(t *testing.T) {
	extra := cmdEmit("extra", module.KindOptIn, "hi", "yo")

	// No ModuleView row: the opt-in module is off, so its command must not run.
	pub := &fakePublisher{}
	p := newPipelineWith(pub, fakeReader{}, extra)
	require.NoError(t, p.Process(chatMsg(t, "standard", "!hi")))
	assert.Empty(t, pub.got, "opt-in command must not run while its module is disabled")

	// ModuleView enables it: the command now runs.
	pub2 := &fakePublisher{}
	p2 := newPipelineWith(pub2, fakeReader{modules: []projection.ModuleView{{Name: "extra", IsEnabled: true}}}, extra)
	require.NoError(t, p2.Process(chatMsg(t, "standard", "!hi")))
	require.Len(t, pub2.got, 1)
	assert.Equal(t, "yo", chatMessageText(t, pub2.got[0].msg))
}

func TestDefaultCommandGatedByModule(t *testing.T) {
	extra := cmdEmit("greet", module.KindDefault, "hey", "hello")

	// No row: a default module ships enabled, so its command runs.
	pub := &fakePublisher{}
	p := newPipelineWith(pub, fakeReader{}, extra)
	require.NoError(t, p.Process(chatMsg(t, "standard", "!hey")))
	require.Len(t, pub.got, 1)

	// Row disables it: the command must not run.
	pub2 := &fakePublisher{}
	p2 := newPipelineWith(pub2, fakeReader{modules: []projection.ModuleView{{Name: "greet", IsEnabled: false}}}, extra)
	require.NoError(t, p2.Process(chatMsg(t, "standard", "!hey")))
	assert.Empty(t, pub2.got)
}
