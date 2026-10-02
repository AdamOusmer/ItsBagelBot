// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"errors"
	"strconv"
	"sync"
	"testing"

	"ItsBagelBot/app/twitch/sesame/module"
	"ItsBagelBot/internal/domain/event/data"
	"ItsBagelBot/pkg/bus"
	"ItsBagelBot/pkg/codec"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
)

type bumpCall struct {
	broadcasterID uint64
	name          string
	delta         int64
}

type fakeBumper struct {
	mu  sync.Mutex
	got []bumpCall
}

func (b *fakeBumper) BumpBot(name string, delta int64) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.got = append(b.got, bumpCall{name: name, delta: delta})
}

func (b *fakeBumper) BumpChannel(broadcasterID uint64, name string, delta int64) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.got = append(b.got, bumpCall{broadcasterID: broadcasterID, name: name, delta: delta})
}

func (b *fakeBumper) calls() []bumpCall {
	b.mu.Lock()
	defer b.mu.Unlock()
	return append([]bumpCall(nil), b.got...)
}

func (b *fakeBumper) botTotals() map[string]int64 {
	var out map[string]int64
	for _, c := range b.calls() {
		if c.broadcasterID != 0 {
			continue
		}
		if out == nil {
			out = map[string]int64{}
		}
		out[c.name] += c.delta
	}
	return out
}

func (b *fakeBumper) channelTotals() map[uint64]map[string]int64 {
	var out map[uint64]map[string]int64
	for _, c := range b.calls() {
		if c.broadcasterID == 0 {
			continue
		}
		if out == nil {
			out = map[uint64]map[string]int64{}
		}
		if out[c.broadcasterID] == nil {
			out[c.broadcasterID] = map[string]int64{}
		}
		out[c.broadcasterID][c.name] += c.delta
	}
	return out
}

func statsPipeline(bumper CounterBumper, reader fakeReader, mods ...module.Module) *Pipeline {
	d := Deps{
		Proj:     reader,
		Live:     liveAlways{},
		Cooldown: NoopCooldown{},
		Pub:      &rawPublisher{},
		Log:      zap.NewNop(),
		Stats:    bumper,
	}
	return NewPipeline(d, NewRegistry(zap.NewNop(), mods...), Config{OutgressPremium: premiumSubj, OutgressStandard: standardSubj})
}

func processed(events, messages int64) map[string]int64 {
	var out map[string]int64
	for name, n := range map[string]int64{counterEventsProcessed: events, counterMessagesProcessed: messages} {
		if n == 0 {
			continue
		}
		if out == nil {
			out = map[string]int64{}
		}
		out[name] = n
	}
	return out
}

func TestProcessCountsReachTheBumper(t *testing.T) {
	cases := []struct {
		name         string
		lines        []string
		reader       fakeReader
		modules      []module.Module
		wantErr      bool
		wantBot      map[string]int64
		wantChannels map[uint64]map[string]int64
	}{
		{
			name:         "chat counts as a message and an event",
			lines:        []string{"chat"},
			wantBot:      processed(1, 1),
			wantChannels: map[uint64]map[string]int64{123: processed(1, 1)},
		},
		{
			name:         "events count per channel",
			lines:        []string{"chat", "event"},
			wantBot:      processed(2, 1),
			wantChannels: map[uint64]map[string]int64{123: processed(2, 1)},
		},
		{
			name:         "a filtered event counts only as an event",
			lines:        []string{"event"},
			wantBot:      processed(1, 0),
			wantChannels: map[uint64]map[string]int64{123: processed(1, 0)},
		},
		{
			name:         "a squashed cohort counts as every message in it",
			lines:        []string{"cohort"},
			wantBot:      processed(7, 7),
			wantChannels: map[uint64]map[string]int64{123: processed(7, 7)},
		},
		{name: "a malformed envelope counts nothing", lines: []string{"bad"}},
		{
			name:         "the envelope is counted before the module views load",
			lines:        []string{"chat"},
			reader:       fakeReader{modErr: errors.New("projection down")},
			modules:      []module.Module{emitModule("greeter", module.KindDefault, "pong")},
			wantErr:      true,
			wantBot:      processed(1, 1),
			wantChannels: map[uint64]map[string]int64{123: processed(1, 1)},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			bumper := &fakeBumper{}
			p := statsPipeline(bumper, tc.reader, tc.modules...)
			msgs := map[string]*bus.Message{
				"chat":   chatMsg(t, "standard", "hi"),
				"event":  eventMsg(t, "stream.online"),
				"cohort": cohortMsg(t, 7, "hello", nil),
				"bad":    bus.NewMessage("uuid-bad", []byte("{not json")),
			}
			var failed bool
			for _, line := range tc.lines {
				failed = failed || p.Process(msgs[line]) != nil
			}
			p.Close()

			assert.Equal(t, tc.wantErr, failed)
			assert.Equal(t, tc.wantBot, bumper.botTotals())
			assert.Equal(t, tc.wantChannels, bumper.channelTotals())
		})
	}
}

func TestProcessStopsTrackingNewChannelsAtTheCap(t *testing.T) {
	bumper := &fakeBumper{}
	p := statsPipeline(bumper, fakeReader{})
	for id := 1; id <= channelStatsMaxKeys+1; id++ {
		line := envelopeMsg(t, "uuid-"+strconv.Itoa(id), map[string]any{
			"broadcaster_user_id": strconv.Itoa(id), "chatter_user_id": "999", "text": "hi",
		})
		require.NoError(t, p.Process(line))
	}
	p.Close()

	channels := bumper.channelTotals()

	assert.Len(t, channels, channelStatsMaxKeys)
	assert.NotContains(t, channels, uint64(channelStatsMaxKeys+1))
	assert.Equal(t, processed(channelStatsMaxKeys+1, channelStatsMaxKeys+1), bumper.botTotals(), "the bot totals still count every message")
}

func TestStatsReachTheLoyaltyReporterUnderTheirOwnScopes(t *testing.T) {
	pub := &rawPublisher{}
	r := NewLoyaltyReporter(pub, zap.NewNop())
	p := statsPipeline(r, fakeReader{})
	require.NoError(t, p.Process(chatMsg(t, "standard", "hi")))
	p.Close()
	r.Close()

	published := pub.payloads[data.SubjectLoyaltyCounters]
	require.Len(t, published, 2)
	scopes := map[uint64][]string{}
	for _, payload := range published {
		var dto data.CounterBumpedDTO
		require.NoError(t, codec.Unmarshal(payload, &dto))
		for _, b := range dto.Bumps {
			scopes[dto.UserID] = append(scopes[dto.UserID], b.Scope)
		}
	}
	assert.Equal(t, map[uint64][]string{
		0:   {data.CounterScopeBot, data.CounterScopeBot},
		123: {data.CounterScopeChannel, data.CounterScopeChannel},
	}, scopes)
}

func loggedFlagFields(logs *observer.ObservedLogs) (fleet map[string]int64, channels map[uint64][2]int64) {
	for _, entry := range logs.All() {
		fields := entry.ContextMap()
		switch entry.Message {
		case "automod detection flags":
			fleet = map[string]int64{}
			for name, value := range fields {
				fleet[name] = value.(int64)
			}
		case "automod detection flags by channel":
			channels = map[uint64][2]int64{}
			for _, e := range fields["channels"].([]any) {
				m := e.(map[string]any)
				channels[m["broadcaster_id"].(uint64)] = [2]int64{m["flags_total"].(int64), m["flags_enforced"].(int64)}
			}
		}
	}
	return fleet, channels
}
