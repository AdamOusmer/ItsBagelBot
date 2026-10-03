// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"context"
	"strconv"
	"testing"
	"time"

	"ItsBagelBot/app/twitch/sesame/module"
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

type chatLineInput struct {
	chatter string
	text    string
	badges  []map[string]string
}

func processChat(t *testing.T, p *Pipeline, in chatLineInput) {
	t.Helper()
	body := map[string]any{
		"type":                chatType,
		"lane":                "standard",
		"broadcaster_user_id": "123",
		"chatter_user_id":     in.chatter,
		"text":                in.text,
	}
	if in.badges != nil {
		body["badges"] = in.badges
	}
	payload, err := codec.Marshal(body)
	require.NoError(t, err)
	require.NoError(t, p.Process(bus.NewMessage("u-"+in.chatter+":"+in.text, payload)))
}

func chatFrom(t *testing.T, p *Pipeline, chatter, text string) {
	t.Helper()
	processChat(t, p, chatLineInput{chatter: chatter, text: text})
}

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

func moderatorNuke(t *testing.T, p *Pipeline, args string) {
	t.Helper()
	processChat(t, p, chatLineInput{
		chatter: "777",
		text:    "!nuke " + args,
		badges:  []map[string]string{{"set_id": "moderator"}},
	})
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

type nukeTimeout struct {
	user    string
	seconds int
	reason  string
}

func nukeTimeouts(t *testing.T, pub *fakePublisher) []nukeTimeout {
	t.Helper()
	var timeouts []nukeTimeout
	for _, c := range pub.snapshot() {
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
		timeouts = append(timeouts, nukeTimeout{body.Data.UserID, body.Data.Duration, body.Data.Reason})
	}
	return timeouts
}

func timeoutTargets(t *testing.T, pub *fakePublisher) []string {
	t.Helper()
	var ids []string
	for _, timeout := range nukeTimeouts(t, pub) {
		assert.Equal(t, nukeTimeout{timeout.user, nukeDefaultSeconds, "nuke"}, timeout)
		ids = append(ids, timeout.user)
	}
	return ids
}

type recentRecord struct {
	env *lane.Envelope
	at  time.Time
}

func recordedAt(at time.Time, envs ...*lane.Envelope) []recentRecord {
	records := make([]recentRecord, len(envs))
	for i, env := range envs {
		records[i] = recentRecord{env: env, at: at}
	}
	return records
}

func fillerRecords(n int) []recentRecord {
	records := make([]recentRecord, n)
	for i := range records {
		records[i] = recentRecord{
			env: soloChatEnv(strconv.Itoa(i), "filler "+strconv.Itoa(i)),
			at:  nukeClockBase.Add(time.Duration(i) * time.Millisecond),
		}
	}
	return records
}

func TestRecentLogSweep(t *testing.T) {
	phraseLine := recordedAt(nukeClockBase, soloChatEnv("999", "FREE N1TRO!!! claim it"), soloChatEnv("998", "totally clean chat"))
	bass := recordedAt(nukeClockBase, soloChatEnv("999", "grabbing bass vibes"))
	raid := soloChatEnv("999", "raid plan meet here")
	commands := recordedAt(nukeClockBase, soloChatEnv("999", "!nuke spam"), soloChatEnv("998", "  !ping"))
	ringSweep := nukeClockBase.Add(time.Duration(recentRingCap+20) * time.Millisecond)
	cases := []struct {
		name    string
		records []recentRecord
		channel channelID
		phrase  string
		at      time.Time
		want    []channelID
	}{
		{"a sweep matches the normalized phrase", phraseLine, 123, "free nitro", nukeClockBase, []channelID{999}},
		{"a sweep misses a partial token", bass, 123, "ass", nukeClockBase, nil},
		{"a sweep hits a whole token", bass, 123, "bass", nukeClockBase, []channelID{999}},
		{
			"one user with two lines is one hit",
			[]recentRecord{{raid, nukeClockBase}, {raid, nukeClockBase.Add(time.Second)}},
			123, "raid plan", nukeClockBase.Add(time.Second), []channelID{999},
		},
		{"past the TTL nothing sweeps", recordedAt(nukeClockBase, raid), 123, "raid plan", nukeClockBase.Add(recentTTL + time.Minute), nil},
		{
			"a cohort's senders are recorded individually",
			recordedAt(nukeClockBase, cohortChatEnv([]string{"1", "2", "3"}, "same copypasta everywhere")),
			123, "copypasta", nukeClockBase, []channelID{1, 2, 3},
		},
		{"a command line is never sweepable", commands, 123, "spam", nukeClockBase, nil},
		{"a command shape is never sweepable even by its own text", commands, 123, "!ping", nukeClockBase, nil},
		{"channels are isolated", recordedAt(nukeClockBase, soloChatEnv("999", "secret phrase x")), 456, "secret phrase", nukeClockBase, nil},
		{"the ring evicts the oldest lines", fillerRecords(recentRingCap + 10), 123, "filler 3", ringSweep, nil},
		{"the ring keeps the newest lines", fillerRecords(recentRingCap + 10), 123, "filler 130", ringSweep, []channelID{130}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			l := NewRecentLog()
			for _, r := range tc.records {
				l.Record(123, r.env, r.at)
			}

			var got []channelID
			for _, hit := range l.Sweep(context.Background(), tc.channel, tc.phrase, tc.at) {
				got = append(got, hit.UserID)
			}

			assert.ElementsMatch(t, tc.want, got)
		})
	}
}

func TestRecentSweepCapsResults(t *testing.T) {
	l := NewRecentLog()
	for i := 0; i < recentRingCap+50; i++ {
		l.Record(123, soloChatEnv(strconv.Itoa(i), "nuke bait"), nukeClockBase)
	}
	assert.Len(t, l.Sweep(context.Background(), 123, "nuke bait", nukeClockBase), recentRingCap,
		"the ring, not the match count, bounds a sweep")
}

func TestNukeTimesOutMatchedChattersAndReports(t *testing.T) {
	n := newNukeUnderTest()
	pub := &fakePublisher{}
	p := nukeTestPipeline(pub, n, nukeModule(n))

	chatFrom(t, p, "111", "join my free nitro giveaway now")
	chatFrom(t, p, "222", "totally innocent message")
	chatFrom(t, p, "333", "FREE NITRO over here!!")
	moderatorNuke(t, p, "free nitro")

	assert.ElementsMatch(t, []string{"111", "333"}, timeoutTargets(t, pub))
	reports := pub.chatTexts(t)
	require.Len(t, reports, 1, "exactly one summary line")
	assert.Contains(t, reports[0], "2 user(s)")
}

func TestNukeNeverTouchesStaffBroadcasterOrBot(t *testing.T) {
	n := newNukeUnderTest()
	pub := &fakePublisher{}
	p := nukeTestPipeline(pub, n, nukeModule(n))

	processChat(t, p, chatLineInput{chatter: "555", text: "free nitro friends", badges: []map[string]string{{"set_id": "vip"}}})
	processChat(t, p, chatLineInput{chatter: "666", text: "free nitro friends", badges: []map[string]string{{"set_id": "moderator"}}})
	chatFrom(t, p, "123", "free nitro friends")
	chatFrom(t, p, "888", "free nitro friends")
	moderatorNuke(t, p, "free nitro")

	assert.Equal(t, []string{"888"}, timeoutTargets(t, pub))
}

func TestNukeOverflowEscalatesShieldOncePerWindow(t *testing.T) {
	cases := []struct {
		name        string
		armShield   bool
		wantShields int
	}{
		{"an armed shield policy activates once on overflow", true, 1},
		{"no armed policy means no activation", false, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			n := newNukeUnderTest()
			pub := &fakePublisher{}
			p := nukeTestPipeline(pub, n, nukeModule(n))
			shieldCalls := 0
			n.shield = nil
			if tc.armShield {
				n.setShield(func(uint64) bool { shieldCalls++; return true })
			}
			for i := 0; i < nukeMaxTargets+5; i++ {
				chatFrom(t, p, strconv.Itoa(1000+i), "the raid has arrived brothers")
			}

			moderatorNuke(t, p, "raid has arrived")

			assert.Len(t, timeoutTargets(t, pub), nukeMaxTargets, "the budget cap holds")
			assert.Equal(t, tc.wantShields, pub.types()[outgress.TypeShieldMode])
			assert.Equal(t, tc.wantShields, shieldCalls)
		})
	}
}

func TestNukeRepliesWithoutActing(t *testing.T) {
	cases := []struct {
		name      string
		service   bool
		args      []string
		wantChats int
	}{
		{"zero hits and a too-short phrase each get a reply", true, []string{"nothing matches this", "ab"}, 2},
		{"inert without a service means silent, not chatty, even on a match", false, []string{"free nitro"}, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var n *Nuke
			if tc.service {
				n = newNukeUnderTest()
			}
			pub := &fakePublisher{}
			p := nukeTestPipeline(pub, n, nukeModule(n))
			chatFrom(t, p, "111", "free nitro everyone come")

			for _, args := range tc.args {
				moderatorNuke(t, p, args)
			}

			assert.Empty(t, timeoutTargets(t, pub))
			assert.Len(t, pub.chatTexts(t), tc.wantChats)
		})
	}
}

func TestNukeClampsTheRequestedDuration(t *testing.T) {
	cases := []struct {
		args string
		want int
	}{
		{"free nitro", nukeDefaultSeconds},
		{"free nitro 300", 300},
		{"free nitro 300s", 300},
		{"free nitro 1", nukeMinSeconds},
		{"free nitro 99999999", nukeMaxSeconds},
	}
	for _, tc := range cases {
		t.Run(tc.args, func(t *testing.T) {
			n := newNukeUnderTest()
			pub := &fakePublisher{}
			p := nukeTestPipeline(pub, n, nukeModule(n))
			chatFrom(t, p, "111", "join my free nitro giveaway now")

			moderatorNuke(t, p, tc.args)

			assert.Equal(t, []nukeTimeout{{"111", tc.want, "nuke"}}, nukeTimeouts(t, pub))
		})
	}
}
