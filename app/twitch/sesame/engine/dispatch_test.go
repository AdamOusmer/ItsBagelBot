// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"context"
	"testing"

	"ItsBagelBot/app/twitch/sesame/module"
	"ItsBagelBot/internal/domain/event/lane"
	"ItsBagelBot/internal/domain/outgress"
	"ItsBagelBot/internal/projection"
	"ItsBagelBot/pkg/codec"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

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
	require.NoError(t, codec.Unmarshal(m.Payload, &inner))
	return inner.Message
}

func customPipeline(resp, perm string) *Pipeline {
	reg := NewRegistry(zap.NewNop())
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

func TestCustomTokensUseDisplayName(t *testing.T) {
	p := customPipeline("{channel}: {user}/{sender}", "everyone")
	c := chatCtx("!so", "")
	c.Env.ChatterUserName = "Alice"
	c.Env.BroadcasterUserName = "StreamerName"
	got := collectDispatch(p, c)
	require.Len(t, got, 1)
	assert.Equal(t, "StreamerName: Alice/Alice", got[0].Text)
}

func TestCustomTokensFallBackToLogin(t *testing.T) {
	p := customPipeline("{user}", "everyone")
	got := collectDispatch(p, chatCtx("!so", ""))
	require.Len(t, got, 1)
	assert.Equal(t, "alice", got[0].Text)
}

func TestCustomAnnounceEmptySkipped(t *testing.T) {
	p := customPipeline("/announce", "everyone")
	assert.Empty(t, collectDispatch(p, chatCtx("!so", "moderator")))
}

func TestCustomPinUntilStreamEnds(t *testing.T) {
	p := customPipeline("/pin Current speed: {args}", "everyone")
	got := collectDispatch(p, chatCtx("!speed 42 km/h", ""))
	require.Len(t, got, 1)
	assert.Equal(t, outgress.TypePin, got[0].Type)
	assert.Equal(t, "Current speed: 42 km/h", got[0].Text)

	msg, err := buildOutgressMessage(&got[0])
	require.NoError(t, err)
	assert.Equal(t, outgress.TypePin, msg.Type)
	assert.Equal(t, "Current speed: 42 km/h", chatMessageText(t, msg))
}

func TestCustomPlainChatStillEmits(t *testing.T) {
	p := customPipeline("hello {sender}", "everyone")
	got := collectDispatch(p, chatCtx("!so", ""))
	require.Len(t, got, 1)
	assert.Equal(t, outgress.TypeChat, got[0].Type)
}

func TestCustomMultiLineEmitsOnePerLine(t *testing.T) {
	p := customPipeline("first {user}\nsecond line\n/announce third", "everyone")
	c := chatCtx("!so", "")
	c.Env.MsgID = "event-message-1"
	got := collectDispatch(p, c)
	require.Len(t, got, 1)
	assert.Equal(t, outgress.TypeBatch, got[0].Type)
	assert.Equal(t, "event-message-1", got[0].BatchID)
	require.Len(t, got[0].Items, 3)
	assert.Equal(t, outgress.TypeChat, got[0].Items[0].Type)
	assert.Equal(t, "first alice", got[0].Items[0].Text)
	assert.Equal(t, outgress.TypeChat, got[0].Items[1].Type)
	assert.Equal(t, "second line", got[0].Items[1].Text)
	assert.Equal(t, outgress.TypeAnnounce, got[0].Items[2].Type)
	assert.Equal(t, "third", got[0].Items[2].Text)
}

func TestCustomMultiLineCappedAtMax(t *testing.T) {
	p := customPipeline("1\n2\n3\n4\n5\n6\n7", "everyone")
	got := collectDispatch(p, chatCtx("!so", ""))
	require.Len(t, got, 1)
	require.Len(t, got[0].Items, 5)
	assert.Equal(t, "5", got[0].Items[4].Text)
}

func TestCustomMultiLineSkipsEmptyLines(t *testing.T) {
	p := customPipeline("one\n\n/announce\ntwo", "everyone")
	c := chatCtx("!so", "")
	c.Env.MsgID = "event-message-2"
	got := collectDispatch(p, c)
	require.Len(t, got, 1)
	require.Len(t, got[0].Items, 2)
	assert.Equal(t, "one", got[0].Items[0].Text)
	assert.Equal(t, "two", got[0].Items[1].Text)
}

func TestCustomIfEmptiedLineIsDropped(t *testing.T) {
	p := customPipeline("hi {user}\n{if:1: you said something}\nlast line", "everyone")
	c := chatCtx("!so", "")
	c.Env.MsgID = "event-message-if"
	got := collectDispatch(p, c)
	require.Len(t, got, 1)
	require.Len(t, got[0].Items, 2, "the emptied line is dropped, the other two are not")
	assert.Equal(t, "hi alice", got[0].Items[0].Text)
	assert.Equal(t, "last line", got[0].Items[1].Text)
}

func TestCustomIfEmptiedLineDoesNotEatTheCap(t *testing.T) {
	p := customPipeline("one\n{if:1:two}\nthree\nfour\nfive", "everyone")
	got := collectDispatch(p, chatCtx("!so", ""))
	require.Len(t, got, 1)
	require.Len(t, got[0].Items, 4)
	assert.Equal(t, "five", got[0].Items[3].Text)
}

func TestCustomIfWholeReplyNeverCollapses(t *testing.T) {
	p := customPipeline("{if:1:you said something}", "everyone")
	assert.Empty(t, collectDispatch(p, chatCtx("!so", "")))

	withArg := customPipeline("{if:1:you said one:you said nothing}", "everyone")
	got := collectDispatch(withArg, chatCtx("!so", ""))
	require.Len(t, got, 1)
	assert.Equal(t, "you said nothing", got[0].Text)
}

func TestCustomMultiLineSuppressionDoesNotLeaveSequenceGap(t *testing.T) {
	p := customPipeline("grabify.link/bad\nsafe line", "everyone")
	c := chatCtx("!so", "")
	c.Env.MsgID = "event-message-3"
	got := collectDispatch(p, c)
	require.Len(t, got, 1)
	assert.Equal(t, "safe line", got[0].Text)
	assert.Equal(t, outgress.TypeChat, got[0].Type, "one surviving line does not need a batch")
}

func TestCustomMultiLineBatchSurvivesPublish(t *testing.T) {
	pub := &fakePublisher{}
	reader := fakeReader{
		cmd:      projection.Command{Name: "raid", Response: "line one\nline two", IsActive: true, Perm: "everyone"},
		cmdFound: true,
	}
	p := newPipelineWith(pub, reader)

	require.NoError(t, p.Process(chatMsg(t, "standard", "!raid")))
	require.Len(t, pub.got, 1)
	assert.Equal(t, outgress.TypeBatch, pub.got[0].msg.Type)
	var batch outgress.Batch
	require.NoError(t, codec.Unmarshal(pub.got[0].msg.Payload, &batch))
	assert.NotEmpty(t, batch.ID)
	require.Len(t, batch.Items, 2)
	assert.Equal(t, outgress.TypeChat, batch.Items[0].Type)
	assert.Equal(t, outgress.TypeChat, batch.Items[1].Type)
}

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

func TestBakedReplyAlwaysHitsTheLexer(t *testing.T) {
	p := newPipelineWith(&fakePublisher{}, fakeReader{},
		cmdEmit("", module.KindCore, "sr", "@{user} the music lookup is down"))
	got := collectDispatch(p, chatCtx("!sr brightside", ""))
	require.Len(t, got, 1)
	assert.Equal(t, "@alice the music lookup is down", got[0].Text)

	choice := newPipelineWith(&fakePublisher{}, fakeReader{},
		cmdEmit("", module.KindCore, "pick", "{choice:yes,yes} @{user}"))
	got = collectDispatch(choice, chatCtx("!pick", ""))
	require.Len(t, got, 1)
	assert.Equal(t, "yes @alice", got[0].Text)
}

func TestBakedReplySlashVerbExpandsThenRoutes(t *testing.T) {
	b := module.NewModule("", module.KindCore)
	b.Command("hype").Everyone().Run(func(_ context.Context, c *module.Context, _ string, emit module.Emit) error {
		emit(&module.Output{Type: outgress.TypeChat, BroadcasterID: c.Env.BroadcasterUserID, Text: "/announce @{user} go"})
		return nil
	})
	p := newPipelineWith(&fakePublisher{}, fakeReader{}, b.Build())

	got := collectDispatch(p, chatCtx("!hype", ""))
	require.Len(t, got, 1)
	assert.Equal(t, outgress.TypeAnnounce, got[0].Type)
	assert.Equal(t, "@alice go", got[0].Text)
}

func TestBakedAndCustomShareEmitPath(t *testing.T) {
	const body = "hi {user}\n/announce {args}"
	c := chatCtx("!so raid incoming", "")
	c.Env.MsgID = "shared-emit"

	custom := collectDispatch(customPipeline(body, "everyone"), c)
	baked := collectDispatch(
		newPipelineWith(&fakePublisher{}, fakeReader{}, cmdEmit("", module.KindCore, "so", body)),
		c,
	)
	require.Equal(t, custom, baked)
}

func TestBakedCommandPermGate(t *testing.T) {
	b := module.NewModule("", module.KindCore)
	b.Command("clear").Mod().Run(func(_ context.Context, c *module.Context, _ string, emit module.Emit) error {
		emit(&module.Output{Type: outgress.TypeChat, BroadcasterID: c.Env.BroadcasterUserID, Text: "ok"})
		return nil
	})
	p := newPipelineWith(&fakePublisher{}, fakeReader{}, b.Build())

	assert.Empty(t, collectDispatch(p, chatCtx("!clear", "")))
	require.Len(t, collectDispatch(p, chatCtx("!clear", "moderator")), 1)
}

func TestBakedDailyShadowing(t *testing.T) {
	enabled := map[string]projection.ModuleView{"urchin": {Name: "urchin", IsEnabled: true}}
	cases := []struct {
		name    string
		beta    bool
		views   map[string]projection.ModuleView
		regress module.Regress
		want    string
	}{
		{"disabled falls through to custom", false, nil, module.RegressStandard, "custom daily"},
		{"enabled wins over custom", false, enabled, module.RegressStandard, "baked daily"},
		{"beta standard lane falls through", true, enabled, module.RegressStandard, "custom daily"},
		{"beta premium lane runs", true, enabled, module.RegressPremium, "baked daily"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			reader := fakeReader{cmd: projection.Command{Name: "daily", Response: "custom daily", IsActive: true, Perm: "everyone"}, cmdFound: true}
			mod := cmdEmit("urchin", module.KindOptIn, "daily", "baked daily")
			mod.Beta = tc.beta
			p := newPipelineWith(&fakePublisher{}, reader, mod)
			c := chatCtx("!daily", "")
			c.Regress = tc.regress
			var got []module.Output
			require.NoError(t, p.dispatchCommand(context.Background(), c, tc.views, func(o *module.Output) { got = append(got, *o) }))
			require.Len(t, got, 1)
			assert.Equal(t, tc.want, got[0].Text)
		})
	}
}

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

func TestNamedCoreCommandAlwaysRuns(t *testing.T) {
	sys := cmdEmit("system", module.KindCore, "sys", "ok")
	pub := &fakePublisher{}
	p := newPipelineWith(pub, fakeReader{}, sys)
	require.NoError(t, p.Process(chatMsg(t, "standard", "!sys")))
	require.Len(t, pub.got, 1)
	assert.Equal(t, "ok", chatMessageText(t, pub.got[0].msg))
}

func TestOptInCommandGatedByModule(t *testing.T) {
	extra := cmdEmit("extra", module.KindOptIn, "hi", "yo")

	pub := &fakePublisher{}
	p := newPipelineWith(pub, fakeReader{}, extra)
	require.NoError(t, p.Process(chatMsg(t, "standard", "!hi")))
	assert.Empty(t, pub.got, "opt-in command must not run while its module is disabled")

	pub2 := &fakePublisher{}
	p2 := newPipelineWith(pub2, fakeReader{modules: projection.ModuleMap([]projection.ModuleView{{Name: "extra", IsEnabled: true}})}, extra)
	require.NoError(t, p2.Process(chatMsg(t, "standard", "!hi")))
	require.Len(t, pub2.got, 1)
	assert.Equal(t, "yo", chatMessageText(t, pub2.got[0].msg))
}

func TestDefaultCommandGatedByModule(t *testing.T) {
	extra := cmdEmit("greet", module.KindDefault, "hey", "hello")

	pub := &fakePublisher{}
	p := newPipelineWith(pub, fakeReader{}, extra)
	require.NoError(t, p.Process(chatMsg(t, "standard", "!hey")))
	require.Len(t, pub.got, 1)

	pub2 := &fakePublisher{}
	p2 := newPipelineWith(pub2, fakeReader{modules: projection.ModuleMap([]projection.ModuleView{{Name: "greet", IsEnabled: false}})}, extra)
	require.NoError(t, p2.Process(chatMsg(t, "standard", "!hey")))
	assert.Empty(t, pub2.got)
}
