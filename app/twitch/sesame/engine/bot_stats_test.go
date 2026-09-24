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
	"go.uber.org/zap/zapcore"
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

func (b *fakeBumper) channelCalls() map[uint64]map[string]int64 {
	out := map[uint64]map[string]int64{}
	for _, c := range b.calls() {
		if c.broadcasterID == 0 {
			continue
		}
		if out[c.broadcasterID] == nil {
			out[c.broadcasterID] = map[string]int64{}
		}
		out[c.broadcasterID][c.name] += c.delta
	}
	return out
}

func (b *fakeBumper) calls() []bumpCall {
	b.mu.Lock()
	defer b.mu.Unlock()
	return append([]bumpCall(nil), b.got...)
}

func statsPipeline(t *testing.T, bumper CounterBumper) *Pipeline {
	t.Helper()
	d := Deps{
		Proj:     fakeReader{},
		Live:     liveAlways{},
		Cooldown: NoopCooldown{},
		Pub:      &rawPublisher{},
		Log:      zap.NewNop(),
		Stats:    bumper,
	}
	p := NewPipeline(d, NewRegistry(zap.NewNop()), Config{OutgressPremium: premiumSubj, OutgressStandard: standardSubj})
	t.Cleanup(p.Close)
	return p
}

func eventMsg(t *testing.T, eventType string) *bus.Message {
	t.Helper()
	body, err := codec.Marshal(map[string]any{
		"type":                eventType,
		"lane":                "standard",
		"broadcaster_user_id": "123",
	})
	require.NoError(t, err)
	return bus.NewMessage("uuid-event", body)
}

func TestProcessCountsChatAsMessageAndEvent(t *testing.T) {
	p := statsPipeline(t, &fakeBumper{})

	require.NoError(t, p.Process(chatMsg(t, "standard", "hi")))
	assert.Equal(t, int64(1), publishedStats(t, p, 0)[counterEventsProcessed])
	assert.Equal(t, int64(1), publishedStats(t, p, 0)[counterMessagesProcessed])
}

func TestProcessCountsPerChannel(t *testing.T) {
	p := statsPipeline(t, &fakeBumper{})

	require.NoError(t, p.Process(chatMsg(t, "standard", "hi")))
	require.NoError(t, p.Process(eventMsg(t, "stream.online")))

	totals := publishedStats(t, p, 123)
	assert.Equal(t, int64(2), totals[counterEventsProcessed])
	assert.Equal(t, int64(1), totals[counterMessagesProcessed])
}

func TestProcessCountsFilteredEventOnly(t *testing.T) {
	p := statsPipeline(t, &fakeBumper{})

	require.NoError(t, p.Process(eventMsg(t, "stream.online")))
	assert.Equal(t, int64(1), publishedStats(t, p, 0)[counterEventsProcessed])
	assert.Zero(t, p.stats.messages.Load())
}

func TestProcessCountsNothingForMalformedEnvelope(t *testing.T) {
	p := statsPipeline(t, &fakeBumper{})

	require.NoError(t, p.Process(bus.NewMessage("uuid-bad", []byte("{not json"))))
	assert.Zero(t, p.stats.events.Load())
	assert.Zero(t, p.stats.messages.Load())
}

func TestProcessWithoutStatsSink(t *testing.T) {
	p := newPipelineWith(&fakePublisher{}, fakeReader{}, emitModule("", module.KindCore, "pong"))
	require.Nil(t, p.stats)
	require.NoError(t, p.Process(chatMsg(t, "standard", "hi")))
}

func TestBotStatsFlushBumpsAndResets(t *testing.T) {
	bumper := &fakeBumper{}
	s := newBotStats(bumper)
	s.count(0, chatDelta(1))
	s.count(0, chatDelta(1))
	s.count(0, eventDelta(1))
	s.Close()

	calls := bumper.calls()
	require.Len(t, calls, 2)
	byName := map[string]bumpCall{}
	for _, c := range calls {
		byName[c.name] = c
	}
	assert.Equal(t, int64(3), byName[counterEventsProcessed].delta)
	assert.Equal(t, int64(2), byName[counterMessagesProcessed].delta)

	assert.Zero(t, s.events.Load())
	assert.Zero(t, s.messages.Load())
	s.flush()
	assert.Len(t, bumper.calls(), 2)
}

func TestBotStatsCountAllocFree(t *testing.T) {
	s := newBotStats(&fakeBumper{})
	t.Cleanup(s.Close)

	if avg := testing.AllocsPerRun(1000, func() { s.count(123, chatDelta(1)) }); avg != 0 {
		t.Fatalf("count allocates %.1f allocs/op, must be 0", avg)
	}
}

func TestBotStatsFlushBumpsChannelsAndResets(t *testing.T) {
	bumper := &fakeBumper{}
	s := newBotStats(bumper)
	s.count(123, chatDelta(1))
	s.count(123, eventDelta(1))
	s.count(456, chatDelta(1))
	s.Close()

	per := bumper.channelCalls()
	assert.Equal(t, int64(2), per[123][counterEventsProcessed])
	assert.Equal(t, int64(1), per[123][counterMessagesProcessed])
	assert.Equal(t, int64(1), per[456][counterEventsProcessed])
	assert.Equal(t, int64(1), per[456][counterMessagesProcessed])

	before := len(bumper.calls())
	s.flush()
	assert.Len(t, bumper.calls(), before)
}

func TestBotStatsChannelCapDropsNewChannels(t *testing.T) {
	s := newBotStats(&fakeBumper{})
	t.Cleanup(s.Close)

	for id := uint64(1); id <= channelStatsMaxKeys; id++ {
		s.count(id, eventDelta(1))
	}
	s.count(channelStatsMaxKeys+1, eventDelta(1))

	s.mu.Lock()
	defer s.mu.Unlock()
	assert.Len(t, s.channels, channelStatsMaxKeys)
	assert.NotContains(t, s.channels, uint64(channelStatsMaxKeys+1))
}

func TestBotStatsBumpsPassReporterGuard(t *testing.T) {
	pub := &rawPublisher{}
	r := NewLoyaltyReporter(pub, zap.NewNop())
	s := newBotStats(r)
	s.count(0, chatDelta(1))
	s.Close()
	r.Close()

	published := pub.payloads[data.SubjectLoyaltyCounters]
	require.Len(t, published, 1)
	var dto data.CounterBumpedDTO
	require.NoError(t, codec.Unmarshal(published[0], &dto))
	assert.Equal(t, uint64(0), dto.UserID)
	assert.Len(t, dto.Bumps, 2)
}

func TestBotStatsChannelBumpsPassReporterGuard(t *testing.T) {
	pub := &rawPublisher{}
	r := NewLoyaltyReporter(pub, zap.NewNop())
	s := newBotStats(r)
	s.count(123, chatDelta(1))
	s.Close()
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
	assert.Equal(t, []string{data.CounterScopeBot, data.CounterScopeBot}, scopes[0])
	assert.Equal(t, []string{data.CounterScopeChannel, data.CounterScopeChannel}, scopes[123])
}

func TestProcessCountsEnvelopeBeforeModuleViews(t *testing.T) {
	d := Deps{
		Proj:     fakeReader{modErr: errors.New("projection down")},
		Live:     liveAlways{},
		Cooldown: NoopCooldown{},
		Pub:      &rawPublisher{},
		Log:      zap.NewNop(),
		Stats:    &fakeBumper{},
	}
	reg := NewRegistry(zap.NewNop(), emitModule("greeter", module.KindDefault, "pong"))
	p := NewPipeline(d, reg, Config{OutgressStandard: standardSubj})
	t.Cleanup(p.Close)

	assert.Error(t, p.Process(chatMsg(t, "standard", "hi")))
	assert.Equal(t, int64(1), publishedStats(t, p, 0)[counterEventsProcessed])
	assert.Equal(t, int64(1), publishedStats(t, p, 0)[counterMessagesProcessed])
}

func TestFlagBucketClassification(t *testing.T) {
	tests := []struct {
		rule string
		want string
	}{
		{"ip_logger", "ip_logger"},
		{"scam", "scam"},
		{"phish", "phish"},
		{"heuristic", "heuristic"},
		{"block_term", "block_term"},
		{"council:campaign", "council_campaign"},
		{"shield_mode", "shield_mode"},
		{"lex:hate:slur", "lex_hate"},
		{"lex:harassment:kys", "lex_harassment"},
		{"lex:sexual:x", "lex_sexual"},
		{"lex:profanity:x", "lex_profanity"},
		{"scam+repeat", "scam"},
		{"heuristic+campaign", "heuristic"},
		{"lex:harassment:x+repeat", "lex_harassment"},
		{"scam+campaign+repeat", "scam"},
		{"council:campaign+repeat", "council_campaign"},
		{"mystery:rule:x", "other"},
	}
	for _, tc := range tests {
		assert.Equalf(t, tc.want, string(flagRuleNames[flagBucket(flagRule(tc.rule))]), "rule %q", tc.rule)
	}
}

type flagOp struct {
	broadcasterID uint64
	rule          string
	enforced      bool
}

func TestFlagVerdictCountersAccuracy(t *testing.T) {
	tests := []struct {
		name         string
		ops          []flagOp
		wantTotal    int64
		wantEnforced int64
		wantRules    map[string]int64
		wantChan     map[uint64][2]int64
	}{
		{
			name: "mixed enforced and shadow across channels",
			ops: []flagOp{
				{123, "scam", true},
				{123, "heuristic", false},
				{0, "council:campaign", true},
				{456, "lex:harassment:kys+repeat", true},
			},
			wantTotal:    4,
			wantEnforced: 3,
			wantRules:    map[string]int64{"scam": 1, "heuristic": 1, "council_campaign": 1, "lex_harassment": 1},
			wantChan:     map[uint64][2]int64{123: {2, 1}, 456: {1, 1}},
		},
		{
			name: "unknown rules collapse into other",
			ops: []flagOp{
				{123, "mystery:rule", true},
				{123, "weirder", false},
			},
			wantTotal:    2,
			wantEnforced: 1,
			wantRules:    map[string]int64{"other": 2},
			wantChan:     map[uint64][2]int64{123: {2, 1}},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s := newBotStats(&fakeBumper{})
			t.Cleanup(s.Close)

			for _, op := range tc.ops {
				s.flag(op.broadcasterID, flagRule(op.rule), op.enforced)
			}

			assert.Equal(t, tc.wantTotal, s.flagsTotal.Load())
			assert.Equal(t, tc.wantEnforced, s.flagsEnforced.Load())
			for i, name := range flagRuleNames {
				assert.Equalf(t, tc.wantRules[string(name)], s.flagsByRule[i].Load(), "bucket %s", name)
			}

			s.mu.Lock()
			defer s.mu.Unlock()
			require.Len(t, s.channels, len(tc.wantChan))
			for id, want := range tc.wantChan {
				tally := s.channels[id]
				require.NotNil(t, tally)
				assert.Equalf(t, want[0], tally.flags, "channel %d flags", id)
				assert.Equalf(t, want[1], tally.enforced, "channel %d enforced", id)
			}
		})
	}
}

func TestFlagBucketsRespectSlotCap(t *testing.T) {
	assert.LessOrEqual(t, int(bktCount), flagRuleSlotCap)

	s := newBotStats(&fakeBumper{})
	t.Cleanup(s.Close)

	for i := 0; i < flagRuleSlotCap*10; i++ {
		s.flag(123, flagRule("unknown:"+strconv.Itoa(i)), true)
	}
	assert.Equal(t, int64(flagRuleSlotCap*10), s.flagsByRule[bktOther].Load(), "all fold into other")
	assert.Equal(t, int64(flagRuleSlotCap*10), s.flagsTotal.Load())

	s.mu.Lock()
	defer s.mu.Unlock()
	for i := bktOther + 1; i < flagRuleSlotCap; i++ {
		assert.Zero(t, s.flagsByRule[i].Load())
	}
}

func TestFlagChannelCapDropsNewChannels(t *testing.T) {
	s := newBotStats(&fakeBumper{})
	t.Cleanup(s.Close)

	for id := uint64(1); id <= channelStatsMaxKeys; id++ {
		s.count(id, eventDelta(1))
	}
	s.flag(channelStatsMaxKeys+1, "scam", true)

	s.mu.Lock()
	defer s.mu.Unlock()
	assert.Len(t, s.channels, channelStatsMaxKeys)
	assert.NotContains(t, s.channels, uint64(channelStatsMaxKeys+1))
}

func TestFlagFlushLogsFleetFieldsNotCounters(t *testing.T) {
	core, logs := observer.New(zapcore.DebugLevel)
	bumper := &fakeBumper{}
	s := newBotStats(bumper, zap.New(core))

	s.flag(123, "scam", true)
	s.flag(123, "heuristic", false)
	s.flag(456, "scam+campaign+repeat", false)
	s.Close()

	var line *observer.LoggedEntry
	for i := range logs.All() {
		if logs.All()[i].Message == "automod detection flags" {
			line = &logs.All()[i]
		}
	}
	require.NotNil(t, line)
	if line == nil {
		return
	}
	assert.Equal(t, zapcore.DebugLevel, line.Level)
	assert.Equal(t, map[string]any{
		"flags_total":         int64(3),
		"flags_enforced":      int64(1),
		"flag_rule_scam":      int64(2),
		"flag_rule_heuristic": int64(1),
	}, line.ContextMap())

	for _, c := range bumper.calls() {
		assert.Contains(t,
			[]string{counterEventsProcessed, counterMessagesProcessed, counterCommandsAnswered, counterModActions},
			c.name, "per-rule flag counters must not become loyalty bumps")
		assert.NotContains(t, c.name, "flag_rule_", "rule names must never mint a counter")
	}
}

func TestFlagFlushSkipsIdleWindow(t *testing.T) {
	core, logs := observer.New(zapcore.DebugLevel)
	s := newBotStats(&fakeBumper{}, zap.New(core))
	s.Close()
	assert.Empty(t, logs.All())
}

func TestFlagFlushLogsChannelFields(t *testing.T) {
	core, logs := observer.New(zapcore.DebugLevel)
	s := newBotStats(&fakeBumper{}, zap.New(core))

	s.count(999, chatDelta(1))
	s.flag(123, "scam", true)
	s.flag(123, "heuristic", false)
	s.flag(456, "lex:sexual:x", false)
	s.Close()

	var line *observer.LoggedEntry
	for i := range logs.All() {
		if logs.All()[i].Message == "automod detection flags by channel" {
			line = &logs.All()[i]
		}
	}
	require.NotNil(t, line)
	raw, ok := line.ContextMap()["channels"].([]any)
	require.True(t, ok, "channels must be an array field")
	got := map[uint64][2]int64{}
	for _, e := range raw {
		m := e.(map[string]any)
		id := m["broadcaster_id"].(uint64)
		got[id] = [2]int64{m["flags_total"].(int64), m["flags_enforced"].(int64)}
	}
	assert.Equal(t, map[uint64][2]int64{123: {2, 1}, 456: {1, 0}}, got)
}

func TestBotStatsCountsASquashedCohortAsEveryMessageInIt(t *testing.T) {
	s := newBotStats(&fakeBumper{})
	defer s.Close()
	s.count(123, chatDelta(7))
	s.count(0, eventDelta(1))
	if got := s.messages.Load(); got != 7 {
		t.Fatalf("messages = %d, want 7", got)
	}
	if got := s.events.Load(); got != 8 {
		t.Fatalf("events = %d, want 8", got)
	}
}

func publishedStats(t *testing.T, p *Pipeline, userID uint64) map[string]int64 {
	t.Helper()
	out := map[string]int64{}
	for _, body := range p.pub.(*rawPublisher).payloads[data.SubjectLoyaltyCounters] {
		var dto data.CounterBumpedDTO
		require.NoError(t, codec.Unmarshal(body, &dto))
		if dto.UserID == userID {
			for _, b := range dto.Bumps {
				out[b.Name] += b.Delta
			}
		}
	}
	return out
}

func TestDecodedCounterFailurePreventsSourceSuccessAndPreservesIdentity(t *testing.T) {
	p := statsPipeline(t, &fakeBumper{})
	pub := &uncertainCounterPublisher{fail: true}
	p.pub = pub
	msg := chatMsg(t, "standard", "hi")
	require.Error(t, p.Process(msg))
	require.Len(t, pub.payloads[data.SubjectLoyaltyCounters], 1)
	pub.fail = false
	require.NoError(t, p.Process(msg))
	bodies := pub.payloads[data.SubjectLoyaltyCounters]
	require.Len(t, bodies, 3)
	require.JSONEq(t, string(bodies[0]), string(bodies[1]))
	require.Zero(t, p.stats.events.Load(), "decoded counters must not also enter volatile batching")
}
