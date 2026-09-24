// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"context"
	"errors"
	"sync"
	"testing"

	"ItsBagelBot/app/twitch/sesame/module"
	"ItsBagelBot/internal/domain/outgress"
	"ItsBagelBot/internal/projection"
	"ItsBagelBot/pkg/bus"
	"ItsBagelBot/pkg/codec"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

type captured struct {
	subject string
	id      string
	msg     outgress.Message
}

type fakePublisher struct {
	mu      sync.Mutex
	got     []captured
	failErr error
}

func (p *fakePublisher) PublishOwned(_ context.Context, subject string, payload []byte) error {
	return p.PublishOwnedWithID(context.Background(), subject, "", payload)
}

func (p *fakePublisher) PublishOwnedWithID(_ context.Context, subject, id string, payload []byte) error {
	if p.failErr != nil {
		return p.failErr
	}
	var om outgress.Message
	_ = codec.Unmarshal(payload, &om)
	p.mu.Lock()
	p.got = append(p.got, captured{subject: subject, id: id, msg: om})
	p.mu.Unlock()
	return nil
}

func (p *fakePublisher) snapshot() []captured {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]captured(nil), p.got...)
}

func (p *fakePublisher) Flush(context.Context) error { return nil }
func (p *fakePublisher) Close() error                { return nil }

type fakeReader struct {
	user     projection.User
	modules  map[string]projection.ModuleView
	modErr   error
	cmd      projection.Command
	cmdFound bool
}

func (r fakeReader) User(context.Context, uint64) (projection.User, error) { return r.user, nil }

func (r fakeReader) Modules(context.Context, uint64) (map[string]projection.ModuleView, error) {
	return r.modules, r.modErr
}
func (r fakeReader) Module(ctx context.Context, id uint64, name string) (projection.ModuleView, bool, error) {
	views, err := r.Modules(ctx, id)
	if err != nil {
		return projection.ModuleView{}, false, err
	}
	view, ok := views[name]
	return view, ok, nil
}
func (r fakeReader) Command(context.Context, uint64, string) (projection.Command, bool, error) {
	return r.cmd, r.cmdFound, nil
}

type liveAlways struct{}

func (liveAlways) IsLive(context.Context, uint64) (bool, error)           { return true, nil }
func (liveAlways) SetLive(context.Context, uint64, int64) (bool, error)   { return true, nil }
func (liveAlways) ClearLive(context.Context, uint64, int64) (bool, error) { return true, nil }

const (
	premiumSubj  = "outgress.premium"
	standardSubj = "outgress.standard"
)

func newPipelineWith(pub bus.Publisher, reader projection.Reader, mods ...module.Module) *Pipeline {
	reg := NewRegistry(zap.NewNop(), mods...)
	d := Deps{Proj: reader, Live: liveAlways{}, Cooldown: NoopCooldown{}, Pub: pub, Log: zap.NewNop()}
	return NewPipeline(d, reg, Config{OutgressPremium: premiumSubj, OutgressStandard: standardSubj})
}

func chatMsg(t *testing.T, laneName, text string) *bus.Message {
	t.Helper()
	body, err := codec.Marshal(map[string]any{
		"type":                chatType,
		"lane":                laneName,
		"broadcaster_user_id": "123",
		"chatter_user_id":     "999",
		"text":                text,
	})
	require.NoError(t, err)
	return bus.NewMessage("uuid-1", body)
}

func bareModule(name string, kind module.Kind) module.Module {
	return module.NewModule(name, kind).Build()
}

func emitModule(name string, kind module.Kind, text string) module.Module {
	b := module.NewModule(name, kind)
	b.On(chatType, func(_ context.Context, c *module.Context, emit module.Emit) error {
		o := GetOutput()
		defer PutOutput(o)
		o.Type = outgress.TypeChat
		o.BroadcasterID = c.Env.BroadcasterUserID
		o.Text = text
		emit(o)
		return nil
	})
	return b.Build()
}

func emitLocaleModule(eventType string) module.Module {
	b := module.NewModule("", module.KindCore)
	b.On(eventType, func(_ context.Context, c *module.Context, emit module.Emit) error {
		emit(&module.Output{
			Type:          outgress.TypeChat,
			BroadcasterID: c.Env.BroadcasterUserID,
			Text:          c.Locale,
		})
		return nil
	})
	return b.Build()
}

func errCore() module.Module {
	b := module.NewModule("", module.KindCore)
	b.On(chatType, func(context.Context, *module.Context, module.Emit) error {
		return errors.New("boom")
	})
	return b.Build()
}

func TestEnabledCoreModuleAlwaysRuns(t *testing.T) {
	p := &Pipeline{}
	mctx := &module.Context{Config: []byte("stale")}
	assert.True(t, p.enabled(bareModule("", module.KindCore), nil, mctx))
	assert.Nil(t, mctx.Config)
}

func TestEnabledByKindAndProjection(t *testing.T) {
	type views = map[string]projection.ModuleView
	cases := []struct {
		name       string
		kind       module.Kind
		views      views
		want       bool
		wantConfig []byte
	}{
		{name: "default enabled", kind: module.KindDefault, views: views{"m": {Name: "m", IsEnabled: true, Configs: []byte(`{"x":1}`)}}, want: true, wantConfig: []byte(`{"x":1}`)},
		{name: "default disabled", kind: module.KindDefault, views: views{"m": {Name: "m", IsEnabled: false}}},
		{name: "default without projection", kind: module.KindDefault, want: true},
		{name: "opt-in without projection", kind: module.KindOptIn},
		{name: "opt-in enabled", kind: module.KindOptIn, views: views{"m": {Name: "m", IsEnabled: true, Configs: []byte(`{"m":"hi"}`)}}, want: true, wantConfig: []byte(`{"m":"hi"}`)},
		{name: "opt-in disabled", kind: module.KindOptIn, views: views{"m": {Name: "m", IsEnabled: false}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			mctx := &module.Context{}
			assert.Equal(t, tc.want, (&Pipeline{}).enabled(bareModule("m", tc.kind), tc.views, mctx))
			if tc.wantConfig != nil {
				assert.Equal(t, tc.wantConfig, []byte(mctx.Config))
			}
		})
	}
}

func TestProcessMalformedEnvelopeDropped(t *testing.T) {
	pub := &fakePublisher{}
	p := newPipelineWith(pub, fakeReader{}, emitModule("", module.KindCore, "x"))
	err := p.Process(bus.NewMessage("uuid-bad", []byte("{not json")))
	assert.NoError(t, err)
	assert.Empty(t, pub.got)
}

func TestProcessLoadsLocaleForEventHandlers(t *testing.T) {
	pub := &fakePublisher{}
	p := newPipelineWith(pub, fakeReader{user: projection.User{Locale: "fr"}}, emitLocaleModule("stream.online"))
	body, err := codec.Marshal(map[string]any{
		"type":                "stream.online",
		"lane":                "standard",
		"broadcaster_user_id": "123",
	})
	require.NoError(t, err)

	require.NoError(t, p.Process(bus.NewMessage("uuid-locale", body)))
	require.Len(t, pub.got, 1)
	assert.Equal(t, "fr", chatMessageText(t, pub.got[0].msg))
}

func TestProcessNoModuleAcks(t *testing.T) {
	pub := &fakePublisher{}
	p := newPipelineWith(pub, fakeReader{})
	err := p.Process(chatMsg(t, "premium", "hi"))
	assert.NoError(t, err)
	assert.Empty(t, pub.got)
}

func TestProcessChatEmittedToStandardLane(t *testing.T) {
	pub := &fakePublisher{}
	p := newPipelineWith(pub, fakeReader{}, emitModule("", module.KindCore, "pong"))
	err := p.Process(chatMsg(t, "standard", "hi"))
	require.NoError(t, err)
	require.Len(t, pub.got, 1)
	assert.Equal(t, standardSubj, pub.got[0].subject)
	assert.Equal(t, outgress.TypeChat, pub.got[0].msg.Type)
	assert.Equal(t, "123", pub.got[0].msg.BroadcasterID)

	var inner struct {
		BroadcasterID string `json:"broadcaster_id"`
		Message       string `json:"message"`
	}
	require.NoError(t, codec.Unmarshal(pub.got[0].msg.Payload, &inner))
	assert.Equal(t, "pong", inner.Message)
	assert.Equal(t, "123", inner.BroadcasterID)
}

func TestProcessChatEmittedToPremiumLane(t *testing.T) {
	pub := &fakePublisher{}
	p := newPipelineWith(pub, fakeReader{}, emitModule("", module.KindCore, "pong"))
	err := p.Process(chatMsg(t, "premium", "hi"))
	require.NoError(t, err)
	require.Len(t, pub.got, 1)
	assert.Equal(t, premiumSubj, pub.got[0].subject)
}

func TestProcessModuleErrorSkippedNotNacked(t *testing.T) {
	pub := &fakePublisher{}
	p := newPipelineWith(pub, fakeReader{}, errCore(), emitModule("", module.KindCore, "still here"))
	err := p.Process(chatMsg(t, "standard", "hi"))
	assert.NoError(t, err)
	require.Len(t, pub.got, 1)
	assert.Equal(t, outgress.TypeChat, pub.got[0].msg.Type)
}

func TestProcessPublishErrorNacks(t *testing.T) {
	pub := &fakePublisher{failErr: errors.New("broker down")}
	p := newPipelineWith(pub, fakeReader{}, emitModule("", module.KindCore, "pong"))
	err := p.Process(chatMsg(t, "standard", "hi"))
	assert.Error(t, err)
}

func TestEmitTranslatesSlashVerbOnModulePath(t *testing.T) {
	pub := &fakePublisher{}
	p := newPipelineWith(pub, fakeReader{}, emitModule("", module.KindCore, "/announcegreen big news"))
	require.NoError(t, p.Process(chatMsg(t, "standard", "hi")))
	require.Len(t, pub.got, 1)
	assert.Equal(t, outgress.TypeAnnounce, pub.got[0].msg.Type)
	assert.Equal(t, "green", pub.got[0].msg.Color)

	var inner struct {
		Message string `json:"message"`
	}
	require.NoError(t, codec.Unmarshal(pub.got[0].msg.Payload, &inner))
	assert.Equal(t, "big news", inner.Message)
}

func TestEmitDropsEmptySlashAction(t *testing.T) {
	pub := &fakePublisher{}
	p := newPipelineWith(pub, fakeReader{}, emitModule("", module.KindCore, "/shoutout"))
	require.NoError(t, p.Process(chatMsg(t, "standard", "hi")))
	assert.Empty(t, pub.got)
}

func TestEmitLeavesMePassthrough(t *testing.T) {
	pub := &fakePublisher{}
	p := newPipelineWith(pub, fakeReader{}, emitModule("", module.KindCore, "/me waves"))
	require.NoError(t, p.Process(chatMsg(t, "standard", "hi")))
	require.Len(t, pub.got, 1)
	assert.Equal(t, outgress.TypeChat, pub.got[0].msg.Type)
	assert.Equal(t, "/me waves", chatMessageText(t, pub.got[0].msg))
}

type countingChatLines struct {
	calls []uint64
}

func (c *countingChatLines) CountChatLine(_ context.Context, broadcasterID uint64) {
	c.calls = append(c.calls, broadcasterID)
}

func TestProcessCountsViewerChatLinesOnly(t *testing.T) {
	cases := []struct {
		name      string
		botID     string
		wantCalls []uint64
	}{
		{name: "viewer line", wantCalls: []uint64{123}},
		{name: "bot's own line", botID: "999"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			counter := &countingChatLines{}
			d := Deps{Proj: fakeReader{}, Live: liveAlways{}, Cooldown: NoopCooldown{}, Pub: &fakePublisher{}, Log: zap.NewNop(), ChatLines: counter}
			p := NewPipeline(d, NewRegistry(zap.NewNop()), Config{BotID: tc.botID, OutgressPremium: premiumSubj, OutgressStandard: standardSubj})

			require.NoError(t, p.Process(chatMsg(t, "standard", "hi")))

			assert.Equal(t, tc.wantCalls, counter.calls)
		})
	}
}

func TestProcessDoesNotCountNonChatEvent(t *testing.T) {
	pub := &fakePublisher{}
	counter := &countingChatLines{}
	p := newPipelineWith(pub, fakeReader{}, emitLocaleModule("stream.online"))
	p.chatLineCounter = counter
	body, err := codec.Marshal(map[string]any{
		"type":                "stream.online",
		"lane":                "standard",
		"broadcaster_user_id": "123",
	})
	require.NoError(t, err)

	require.NoError(t, p.Process(bus.NewMessage("uuid-locale", body)))

	assert.Empty(t, counter.calls)
}

func TestProcessNilChatLineCounterIsSafe(t *testing.T) {
	pub := &fakePublisher{}
	p := newPipelineWith(pub, fakeReader{}, emitModule("", module.KindCore, "pong"))
	require.NoError(t, p.Process(chatMsg(t, "standard", "hi")))
}
