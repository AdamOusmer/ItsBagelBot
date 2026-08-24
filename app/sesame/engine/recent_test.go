// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"context"
	"strconv"
	"testing"
	"time"

	"ItsBagelBot/app/sesame/module"
	"ItsBagelBot/internal/domain/event/lane"
	"ItsBagelBot/internal/domain/outgress"
	"ItsBagelBot/pkg/bus"
	"ItsBagelBot/pkg/codec"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

var nukeClockBase = time.Unix(1_800_000_000, 0)

func nukeTestPipeline(pub *fakePublisher, n *Nuke, mods ...module.Module) *Pipeline {
	reg := NewRegistry(zap.NewNop(), mods...)
	d := Deps{Proj: fakeReader{}, Live: liveAlways{}, Cooldown: NoopCooldown{},
		Pub: pub, Log: zap.NewNop(), Nuke: n}
	return NewPipeline(d, reg, Config{OutgressPremium: premiumSubj, OutgressStandard: standardSubj})
}

func newNukeUnderTest() *Nuke {
	n := NewNuke(NewRecentLog(), 42, zap.NewNop())
	n.setClock(func() time.Time { return nukeClockBase })
	return n
}

// chatFrom processes one plain chat line through a recording pipeline.
func chatFrom(t *testing.T, p *Pipeline, uuid, chatter, text string) {
	t.Helper()
	body, err := codec.Marshal(map[string]any{
		"type":                chatType,
		"lane":                "standard",
		"broadcaster_user_id": "123",
		"chatter_user_id":     chatter,
		"text":                text,
	})
	require.NoError(t, err)
	require.NoError(t, p.Process(bus.NewMessage(uuid, body)))
}

func chatWithBadges(t *testing.T, p *Pipeline, uuid, chatter, text string, badges []map[string]string) {
	t.Helper()
	body, err := codec.Marshal(map[string]any{
		"type":                chatType,
		"lane":                "standard",
		"broadcaster_user_id": "123",
		"chatter_user_id":     chatter,
		"text":                text,
		"badges":              badges,
	})
	require.NoError(t, err)
	require.NoError(t, p.Process(bus.NewMessage(uuid, body)))
}

// nukeModule mirrors the production moderation module's registration so the
// dispatch path (gate, cooldown, emit middleware) is exercised for real; the
// engine test package cannot import app/sesame/modules without a cycle.
func nukeModule(n *Nuke) module.Module {
	b := module.NewModule("moderation", module.KindDefault)
	b.Command("nuke").Mod().Cooldown(5 * time.Second).Run(func(ctx context.Context, c *module.Context, args string, emit module.Emit) error {
		if n == nil {
			return nil
		}
		return n.Execute(ctx, c, args, emit)
	})
	return b.Build()
}

// moderatorNuke processes "!nuke <args>" from a moderator in channel 123.
func moderatorNuke(t *testing.T, p *Pipeline, args string) {
	t.Helper()
	chatWithBadges(t, p, "uuid-nuke:"+args, "777", "!nuke "+args,
		[]map[string]string{{"set_id": "moderator"}})
}

func soloChatEnv(chatter, text string) *lane.Envelope {
	return &lane.Envelope{Type: chatType, BroadcasterUserID: "123", ChatterUserID: chatter, Text: text}
}

func cohortChatEnv(chatters []string, text string) *lane.Envelope {
	env := &lane.Envelope{Type: chatType, BroadcasterUserID: "123", Text: text}
	for _, c := range chatters {
		env.Senders = append(env.Senders, lane.Sender{ChatterUserID: c})
	}
	return env
}

func timeoutTargets(t *testing.T, pub *fakePublisher) []string {
	t.Helper()
	var ids []string
	for _, c := range pub.got {
		if c.msg.Type != outgress.TypeTimeout {
			continue
		}
		var body struct {
			Data struct {
				UserID   string `json:"user_id"`
				Duration int    `json:"duration"`
				Reason   string `json:"reason"`
			} `json:"data"`
		}
		require.NoError(t, codec.Unmarshal(c.msg.Payload, &body))
		assert.Equal(t, 600, body.Data.Duration)
		assert.Equal(t, "nuke", body.Data.Reason)
		ids = append(ids, body.Data.UserID)
	}
	return ids
}

// --- RecentLog ---

func TestRecentSweepMatchesNormalizedPhrase(t *testing.T) {
	l := NewRecentLog()
	l.Record(123, soloChatEnv("999", "FREE N1TRO!!! claim it"), nukeClockBase)
	l.Record(123, soloChatEnv("998", "totally clean chat"), nukeClockBase)

	hits := l.Sweep(context.Background(), 123, "free nitro", nukeClockBase)
	require.Len(t, hits, 1)
	assert.Equal(t, uint64(999), hits[0].UserID)
	assert.Equal(t, module.RoleEveryone, hits[0].Role)
}

func TestRecentSweepBoundaryMissesPartialToken(t *testing.T) {
	l := NewRecentLog()
	l.Record(123, soloChatEnv("999", "grabbing bass vibes"), nukeClockBase)
	assert.Empty(t, l.Sweep(context.Background(), 123, "ass", nukeClockBase))
	assert.Len(t, l.Sweep(context.Background(), 123, "bass", nukeClockBase), 1)
}

func TestRecentSweepDedupesAndExpires(t *testing.T) {
	l := NewRecentLog()
	env := soloChatEnv("999", "raid plan meet here")
	l.Record(123, env, nukeClockBase)
	l.Record(123, env, nukeClockBase.Add(time.Second))
	assert.Len(t, l.Sweep(context.Background(), 123, "raid plan", nukeClockBase.Add(time.Second)), 1, "one user, two lines")

	assert.Empty(t, l.Sweep(context.Background(), 123, "raid plan", nukeClockBase.Add(recentTTL+time.Minute)), "past the TTL nothing sweeps")
}

func TestRecentRecordsCohortSendersIndividually(t *testing.T) {
	l := NewRecentLog()
	l.Record(123, cohortChatEnv([]string{"1", "2", "3"}, "same copypasta everywhere"), nukeClockBase)
	assert.Len(t, l.Sweep(context.Background(), 123, "copypasta", nukeClockBase), 3)
}

func TestRecentSkipsCommandShapes(t *testing.T) {
	l := NewRecentLog()
	l.Record(123, soloChatEnv("999", "!nuke spam"), nukeClockBase)
	l.Record(123, soloChatEnv("998", "  !ping"), nukeClockBase)
	assert.Empty(t, l.Sweep(context.Background(), 123, "spam", nukeClockBase), "a command line must never be sweepable")
	assert.Empty(t, l.Sweep(context.Background(), 123, "!ping", nukeClockBase))
}

func TestRecentChannelsAreIsolated(t *testing.T) {
	l := NewRecentLog()
	l.Record(123, soloChatEnv("999", "secret phrase x"), nukeClockBase)
	assert.Empty(t, l.Sweep(context.Background(), 456, "secret phrase", nukeClockBase))
}

func TestRecentRingEvictsOldestBeyondCap(t *testing.T) {
	l := NewRecentLog()
	for i := 0; i < recentRingCap+10; i++ {
		l.Record(123, soloChatEnv(strconv.Itoa(i), "filler "+strconv.Itoa(i)),
			nukeClockBase.Add(time.Duration(i)*time.Millisecond))
	}
	sweepAt := nukeClockBase.Add(time.Duration(recentRingCap+20) * time.Millisecond)
	assert.Empty(t, l.Sweep(context.Background(), 123, "filler 3", sweepAt), "the oldest lines fell off the ring")
	assert.NotEmpty(t, l.Sweep(context.Background(), 123, "filler 130", sweepAt), "the newest survived")
}

func TestRecentSweepCapsResults(t *testing.T) {
	l := NewRecentLog()
	for i := 0; i < recentRingCap+50; i++ {
		l.Record(123, soloChatEnv(strconv.Itoa(i), "nuke bait"), nukeClockBase)
	}
	assert.Len(t, l.Sweep(context.Background(), 123, "nuke bait", nukeClockBase), recentRingCap,
		"the ring, not the match count, bounds a sweep")
}

// --- !nuke end to end ---

func TestNukeTimesOutMatchedChattersAndReports(t *testing.T) {
	n := newNukeUnderTest()
	pub := &fakePublisher{}
	p := nukeTestPipeline(pub, n, nukeModule(n))

	chatFrom(t, p, "u1", "111", "join my free nitro giveaway now")
	chatFrom(t, p, "u2", "222", "totally innocent message")
	chatFrom(t, p, "u3", "333", "FREE NITRO over here!!")
	moderatorNuke(t, p, "free nitro")

	ids := timeoutTargets(t, pub)
	assert.ElementsMatch(t, []string{"111", "333"}, ids)

	reports := 0
	for _, c := range pub.got {
		if c.msg.Type != outgress.TypeChat {
			continue
		}
		reports++
		var body struct {
			Message string `json:"message"`
		}
		require.NoError(t, codec.Unmarshal(c.msg.Payload, &body))
		assert.Contains(t, body.Message, "2 user(s)")
	}
	assert.Equal(t, 1, reports, "exactly one summary line")
}

func TestNukeNeverTouchesStaffBroadcasterOrBot(t *testing.T) {
	n := newNukeUnderTest()
	pub := &fakePublisher{}
	p := nukeTestPipeline(pub, n, nukeModule(n))

	chatWithBadges(t, p, "u1", "555", "free nitro friends", []map[string]string{{"set_id": "vip"}})
	chatWithBadges(t, p, "u2", "666", "free nitro friends", []map[string]string{{"set_id": "moderator"}})
	chatFrom(t, p, "u3", "123", "free nitro friends") // the broadcaster themself
	chatFrom(t, p, "u4", "888", "free nitro friends")
	moderatorNuke(t, p, "free nitro")

	assert.Equal(t, []string{"888"}, timeoutTargets(t, pub))
}

func TestNukeOverflowEscalatesShieldOncePerWindow(t *testing.T) {
	n := newNukeUnderTest()
	pub := &fakePublisher{}
	p := nukeTestPipeline(pub, n, nukeModule(n))

	shieldCalls := 0
	n.setShield(func(uint64) bool { shieldCalls++; return true })

	for i := 0; i < nukeMaxTargets+5; i++ {
		chatFrom(t, p, "u"+strconv.Itoa(i), strconv.Itoa(1000+i), "the raid has arrived brothers")
	}
	moderatorNuke(t, p, "raid has arrived")

	assert.Len(t, timeoutTargets(t, pub), nukeMaxTargets, "the budget cap holds")

	shields := 0
	for _, c := range pub.got {
		if c.msg.Type == outgress.TypeShieldMode {
			shields++
		}
	}
	assert.Equal(t, 1, shields, "overflow activates Shield Mode once")
	assert.Equal(t, 1, shieldCalls)
}

func TestNukeWithoutShieldArmedStillReportsTheCap(t *testing.T) {
	n := newNukeUnderTest()
	n.shield = nil
	pub := &fakePublisher{}
	p := nukeTestPipeline(pub, n, nukeModule(n))

	for i := 0; i < nukeMaxTargets+1; i++ {
		chatFrom(t, p, "u"+strconv.Itoa(i), strconv.Itoa(1000+i), "spam wave incoming now")
	}
	moderatorNuke(t, p, "spam wave incoming")

	for _, c := range pub.got {
		assert.NotEqual(t, outgress.TypeShieldMode, c.msg.Type, "no armed policy, no activation")
	}
	assert.Len(t, timeoutTargets(t, pub), nukeMaxTargets)
}

func TestNukeZeroHitsAndUsageReplies(t *testing.T) {
	n := newNukeUnderTest()
	pub := &fakePublisher{}
	p := nukeTestPipeline(pub, n, nukeModule(n))

	moderatorNuke(t, p, "nothing matches this")
	moderatorNuke(t, p, "ab")

	timeouts := timeoutTargets(t, pub)
	assert.Empty(t, timeouts)
	chats := 0
	for _, c := range pub.got {
		if c.msg.Type == outgress.TypeChat {
			chats++
		}
	}
	assert.Equal(t, 2, chats, "a reply per invocation, no actions")
}

func TestNukeDurationParsing(t *testing.T) {
	tests := []struct {
		args string
		want int64
	}{
		{"spam wave", nukeDefaultSeconds},
		{"spam wave 300", 300},
		{"spam wave 300s", 300},
		{"spam wave 1", nukeMinSeconds},
		{"spam wave 99999999", nukeMaxSeconds},
	}
	for _, tt := range tests {
		phrase, secs := parseNukeArgs(tt.args)
		assert.Equal(t, tt.want, secs, tt.args)
		assert.Equal(t, "spam wave", phrase, tt.args)
	}
}

func TestNukeInertWithoutService(t *testing.T) {
	pub := &fakePublisher{}
	p := nukeTestPipeline(pub, nil, nukeModule(nil))

	chatFrom(t, p, "u1", "111", "free nitro everyone come")
	moderatorNuke(t, p, "free nitro")

	assert.Empty(t, timeoutTargets(t, pub), "no service wired, no actions")
	for _, c := range pub.got {
		assert.NotEqual(t, outgress.TypeChat, c.msg.Type, "inert means silent, not chatty")
	}
}
