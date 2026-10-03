// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"context"
	"errors"
	"maps"
	"slices"
	"strconv"
	"testing"

	"ItsBagelBot/app/twitch/sesame/module"
	"ItsBagelBot/internal/domain/event/lane"
	"ItsBagelBot/internal/domain/outgress"
	"ItsBagelBot/internal/moderation"
	"ItsBagelBot/internal/projection"
	"ItsBagelBot/pkg/bus"
	"ItsBagelBot/pkg/codec"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func envelopeMsg(t *testing.T, id string, fields map[string]any) *bus.Message {
	t.Helper()
	body := map[string]any{"type": chatType, "lane": "standard", "broadcaster_user_id": "123"}
	maps.Copy(body, fields)
	encoded, err := codec.Marshal(body)
	require.NoError(t, err)
	return bus.NewMessage(id, encoded)
}

func chatMsg(t *testing.T, laneName, text string) *bus.Message {
	t.Helper()
	return envelopeMsg(t, "uuid-1", map[string]any{"lane": laneName, "chatter_user_id": "999", "text": text})
}

func commandMsg(t *testing.T, msgID, text string) *bus.Message {
	t.Helper()
	return envelopeMsg(t, "uuid-"+msgID, map[string]any{"msg_id": msgID, "chatter_user_id": "999", "text": text})
}

func eventMsg(t *testing.T, eventType string) *bus.Message {
	t.Helper()
	return envelopeMsg(t, "uuid-event", map[string]any{"type": eventType})
}

func cohortMsg(t *testing.T, size int, text string, extra map[string]any) *bus.Message {
	t.Helper()
	senders := make([]map[string]any, size)
	for i := range senders {
		senders[i] = map[string]any{"chatter_user_id": strconv.Itoa(i + 1)}
	}
	fields := map[string]any{"text": text, "senders": senders}
	maps.Copy(fields, extra)
	return envelopeMsg(t, "cohort", fields)
}

func chatEnv(text, badgeRole string) lane.Envelope {
	env := lane.Envelope{
		Type:              chatType,
		Lane:              "standard",
		Text:              text,
		BroadcasterUserID: "123",
		ChatterUserID:     "999",
		ChatterUserLogin:  "alice",
	}
	if badgeRole != "" {
		env.Badges = []lane.Badge{{SetID: badgeRole}}
	}
	return env
}

func outputOf(m outgress.Message) module.Output {
	if m.Type != outgress.TypeBatch {
		var inner struct {
			Message string `json:"message"`
		}
		_ = codec.Unmarshal(m.Payload, &inner)
		return module.Output{Type: m.Type, Color: m.Color, Text: inner.Message}
	}
	var batch outgress.Batch
	_ = codec.Unmarshal(m.Payload, &batch)
	out := module.Output{Type: m.Type, BatchID: batch.ID}
	for _, item := range batch.Items {
		out.Items = append(out.Items, outputOf(item))
	}
	return out
}

func runChat(t *testing.T, p *Pipeline, env lane.Envelope) ([]module.Output, error) {
	t.Helper()
	pub := p.pub.(*fakePublisher)
	before := len(pub.snapshot())
	body, err := codec.Marshal(env)
	require.NoError(t, err)

	processErr := p.Process(bus.NewMessage("uuid-chat", body))

	var got []module.Output
	for _, c := range pub.snapshot()[before:] {
		got = append(got, outputOf(c.msg))
	}
	return got, processErr
}

func replies(t *testing.T, p *Pipeline, env lane.Envelope) []module.Output {
	t.Helper()
	got, err := runChat(t, p, env)
	require.NoError(t, err)
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

func configEcho(prefix, name string, kind module.Kind) module.Module {
	b := module.NewModule(name, kind)
	b.On(chatType, func(_ context.Context, c *module.Context, emit module.Emit) error {
		emit(&module.Output{Type: outgress.TypeChat, BroadcasterID: c.Env.BroadcasterUserID, Text: prefix + ":" + string(c.Config)})
		return nil
	})
	return b.Build()
}

type published struct {
	Subject string
	Type    string
	Color   string
	Text    string
}

func publishedMessages(pub *fakePublisher) []published {
	var out []published
	for _, c := range pub.snapshot() {
		var inner struct {
			Message string `json:"message"`
		}
		_ = codec.Unmarshal(c.msg.Payload, &inner)
		out = append(out, published{Subject: c.subject, Type: c.msg.Type, Color: c.msg.Color, Text: inner.Message})
	}
	return out
}

type processCase struct {
	name    string
	lane    string
	text    string
	raw     []byte
	cohort  int
	reader  fakeReader
	modules []module.Module
	pubErr  error
	wantErr bool
	want    []published
}

func (tc processCase) message(t *testing.T) *bus.Message {
	t.Helper()
	switch {
	case tc.raw != nil:
		return bus.NewMessage("uuid-bad", tc.raw)
	case tc.cohort > 0:
		return cohortMsg(t, tc.cohort, tc.text, nil)
	}
	return chatMsg(t, tc.lane, tc.text)
}

func processRoutingCases() []processCase {
	pong := emitModule("", module.KindCore, "pong")

	return []processCase{
		{name: "a malformed envelope is dropped and acked", raw: []byte("{not json"), modules: []module.Module{pong}},
		{name: "a chat line with no module acks and emits nothing", lane: "premium", text: "hi"},
		{
			name: "chat is emitted to the standard lane", lane: "standard", text: "hi", modules: []module.Module{pong},
			want: []published{{standardSubj, outgress.TypeChat, "", "pong"}},
		},
		{
			name: "chat is emitted to the premium lane", lane: "premium", text: "hi", modules: []module.Module{pong},
			want: []published{{premiumSubj, outgress.TypeChat, "", "pong"}},
		},
		{
			name: "a failing module is skipped, not nacked", lane: "standard", text: "hi",
			modules: []module.Module{errCore(), emitModule("", module.KindCore, "still here")},
			want:    []published{{standardSubj, outgress.TypeChat, "", "still here"}},
		},
		{
			name: "a publish error nacks", lane: "standard", text: "hi", modules: []module.Module{pong},
			pubErr: errors.New("broker down"), wantErr: true,
		},
	}
}

func processEmissionCases() []processCase {
	slur := moderation.EmbeddedLexicon().Terms(moderation.CatHate)[0]

	return []processCase{
		{
			name: "TestEmitTranslatesSlashVerbOnModulePath", lane: "standard", text: "hi",
			modules: []module.Module{emitModule("", module.KindCore, "/announcegreen big news")},
			want:    []published{{standardSubj, outgress.TypeAnnounce, "green", "big news"}},
		},
		{
			name: "TestEmitDropsEmptySlashAction", lane: "standard", text: "hi",
			modules: []module.Module{emitModule("", module.KindCore, "/shoutout")},
		},
		{
			name: "an emitted empty pin is dropped", lane: "standard", text: "hi",
			modules: []module.Module{emitModule("", module.KindCore, "/pin")},
		},
		{
			name: "an emitted empty chat line is dropped", lane: "standard", text: "hi",
			modules: []module.Module{emitModule("", module.KindCore, "")},
		},
		{
			name: "TestEmitLeavesMePassthrough", lane: "standard", text: "hi",
			modules: []module.Module{emitModule("", module.KindCore, "/me waves")},
			want:    []published{{standardSubj, outgress.TypeChat, "", "/me waves"}},
		},
		{
			name: "an emission carrying floor content is suppressed", lane: "standard", text: "hello",
			modules: []module.Module{emitModule("", module.KindCore, "so true "+slur+" moment")},
		},
		{
			name: "milder language still goes out", lane: "standard", text: "hello",
			modules: []module.Module{emitModule("", module.KindCore, "hell of a play, that was bullshit ref")},
			want:    []published{{standardSubj, outgress.TypeChat, "", "hell of a play, that was bullshit ref"}},
		},
	}
}

func processCommandCases() []processCase {
	command := fakeReader{cmd: projection.Command{Name: "hi", Response: "hello", IsActive: true}, cmdFound: true}

	return []processCase{
		{name: "a cohort never dispatches a command", text: "!hi", cohort: 2, reader: command},
		{
			name: "a normal command line still dispatches", lane: "standard", text: "!hi", reader: command,
			want: []published{{standardSubj, outgress.TypeChat, "", "hello"}},
		},
	}
}

func processModuleCases() []processCase {
	return []processCase{
		{
			name: "an enabled default module receives its configured blob", lane: "standard", text: "hi",
			modules: []module.Module{configEcho("m", "m", module.KindDefault)},
			reader:  fakeReader{modules: map[string]projection.ModuleView{"m": {Name: "m", IsEnabled: true, Configs: []byte(`{"x":1}`)}}},
			want:    []published{{standardSubj, outgress.TypeChat, "", `m:{"x":1}`}},
		},
		{
			name: "a disabled default module stays silent", lane: "standard", text: "hi",
			modules: []module.Module{configEcho("m", "m", module.KindDefault)},
			reader:  fakeReader{modules: map[string]projection.ModuleView{"m": {Name: "m", IsEnabled: false}}},
		},
		{
			name: "a default module without a projection row runs with no config", lane: "standard", text: "hi",
			modules: []module.Module{configEcho("m", "m", module.KindDefault)},
			want:    []published{{standardSubj, outgress.TypeChat, "", "m:"}},
		},
		{
			name: "an opt-in module without a projection row stays silent", lane: "standard", text: "hi",
			modules: []module.Module{configEcho("m", "m", module.KindOptIn)},
		},
		{
			name: "an enabled opt-in module receives its configured blob", lane: "standard", text: "hi",
			modules: []module.Module{configEcho("m", "m", module.KindOptIn)},
			reader:  fakeReader{modules: map[string]projection.ModuleView{"m": {Name: "m", IsEnabled: true, Configs: []byte(`{"m":"hi"}`)}}},
			want:    []published{{standardSubj, outgress.TypeChat, "", `m:{"m":"hi"}`}},
		},
		{
			name: "a disabled opt-in module stays silent", lane: "standard", text: "hi",
			modules: []module.Module{configEcho("m", "m", module.KindOptIn)},
			reader:  fakeReader{modules: map[string]projection.ModuleView{"m": {Name: "m", IsEnabled: false}}},
		},
		{
			name: "a core module runs beside a configured module without inheriting its config", lane: "standard", text: "hi",
			modules: []module.Module{configEcho("m", "m", module.KindDefault), configEcho("core", "", module.KindCore)},
			reader:  fakeReader{modules: map[string]projection.ModuleView{"m": {Name: "m", IsEnabled: true, Configs: []byte(`{"x":1}`)}}},
			want: []published{
				{standardSubj, outgress.TypeChat, "", `m:{"x":1}`},
				{standardSubj, outgress.TypeChat, "", "core:"},
			},
		},
	}
}

func TestProcessEmitsAtTheBroker(t *testing.T) {
	for _, tc := range slices.Concat(processRoutingCases(), processEmissionCases(), processCommandCases(), processModuleCases()) {
		t.Run(tc.name, func(t *testing.T) {
			pub := &fakePublisher{failErr: tc.pubErr}

			err := newPipelineWith(pub, tc.reader, tc.modules...).Process(tc.message(t))

			assert.Equal(t, tc.wantErr, err != nil)
			assert.Equal(t, tc.want, publishedMessages(pub))
		})
	}
}

func TestProcessLoadsLocaleForEventHandlers(t *testing.T) {
	pub := &fakePublisher{}
	p := newPipelineWith(pub, fakeReader{user: projection.User{Locale: "fr"}}, emitLocaleModule("stream.online"))

	require.NoError(t, p.Process(eventMsg(t, "stream.online")))

	assert.Equal(t, []published{{standardSubj, outgress.TypeChat, "", "fr"}}, publishedMessages(pub))
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
		event     string
		wantCalls []uint64
	}{
		{name: "viewer line", wantCalls: []uint64{123}},
		{name: "bot's own line", botID: "999"},
		{name: "non-chat event", event: "stream.online"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			counter := &countingChatLines{}
			d := Deps{Proj: fakeReader{}, Live: liveAlways{}, Cooldown: NoopCooldown{}, Pub: &fakePublisher{}, Log: zap.NewNop(), ChatLines: counter}
			p := NewPipeline(d, NewRegistry(zap.NewNop()), Config{BotID: tc.botID, OutgressPremium: premiumSubj, OutgressStandard: standardSubj})
			msg := chatMsg(t, "standard", "hi")
			if tc.event != "" {
				msg = eventMsg(t, tc.event)
			}

			require.NoError(t, p.Process(msg))

			assert.Equal(t, tc.wantCalls, counter.calls)
		})
	}
}

func TestProcessFeedsRosterFromChatLines(t *testing.T) {
	p := newPipelineWith(&fakePublisher{}, fakeReader{})
	line := envelopeMsg(t, "uuid-roster", map[string]any{
		"chatter_user_id": "7", "chatter_user_login": "bob", "chatter_user_name": "Bob", "text": "hi",
	})

	require.NoError(t, p.Process(line))

	v, ok := p.roster.Resolve(123, "bob")
	require.True(t, ok)
	assert.Equal(t, Viewer{ID: 7, Login: "bob", Name: "Bob"}, v)
}

func TestProcessOutputIDsFollowTheEventID(t *testing.T) {
	cases := []struct {
		name      string
		eventIDs  []string
		wantIDs   int
		wantEmpty bool
	}{
		{"a replayed event reuses its output id", []string{"event-1", "event-1"}, 1, false},
		{"distinct events use distinct output ids", []string{"event-1", "event-2"}, 2, false},
		{"an event without an id publishes ordinarily", []string{""}, 1, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			pub := &fakePublisher{}
			p := newPipelineWith(pub, fakeReader{}, emitModule("", module.KindCore, "pong"))
			for _, eventID := range tc.eventIDs {
				fields := map[string]any{"chatter_user_id": "999", "text": "hi"}
				if eventID != "" {
					fields["event_id"], fields["msg_id"] = eventID, "chat-message-1"
				}
				require.NoError(t, p.Process(envelopeMsg(t, "uuid-"+eventID, fields)))
			}

			ids := map[string]bool{}
			for _, c := range pub.snapshot() {
				ids[c.id] = true
			}

			assert.Len(t, pub.snapshot(), len(tc.eventIDs))
			assert.Len(t, ids, tc.wantIDs)
			assert.Equal(t, tc.wantEmpty, ids[""])
		})
	}
}
