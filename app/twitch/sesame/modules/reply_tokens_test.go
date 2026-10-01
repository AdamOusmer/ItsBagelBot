// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package modules

import (
	"regexp"
	"testing"

	"ItsBagelBot/app/twitch/sesame/engine"
	"ItsBagelBot/app/twitch/sesame/module"
	gossiprpc "ItsBagelBot/internal/domain/rpc/gossip"
	"ItsBagelBot/internal/projection"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func assertResolved(t *testing.T, ns, text string) {
	t.Helper()
	names, ok := ReplyTokenInventory()[ns]
	require.True(t, ok, "no inventory entry for %q", ns)
	for _, name := range names {
		assert.NotContains(t, text, "{"+name+"}", "%s: %q left unresolved in %q", ns, name, text)
	}
}

func TestReplyTokensResolveInEventReplies(t *testing.T) {
	cases := []struct {
		ns      string
		m       module.Module
		event   string
		payload string
	}{
		{"alerts.follow", Alerts(alertsDeps(nil)), "channel.follow", followJSON},
		{"alerts.sub", Alerts(alertsDeps(nil)), "channel.subscribe", subscribeJSON},
		{"alerts.cheer", Alerts(alertsDeps(nil)), "channel.cheer", cheerJSON},
		{"alerts.raid", Alerts(alertsDeps(nil)), "channel.raid", raidJSON},
		{"shoutout.shoutout", Shoutout(engine.Deps{Log: zap.NewNop()}), "channel.raid", raidJSON},
	}
	for _, tc := range cases {
		t.Run(tc.ns, func(t *testing.T) {
			out := runEvent(t, tc.m, eventCtx(eventInput{tc.event, tc.payload, ""}))
			require.NotEmpty(t, out)
			assertResolved(t, tc.ns, out[0].Text)
		})
	}
}

func TestReplyTokensResolveInChatReplies(t *testing.T) {
	cases := []struct {
		ns     string
		m      module.Module
		text   string
		config string
		badge  string
	}{
		{"time.time", TimeOfDay(engine.Deps{Log: zap.NewNop()}), "!time", `{"timezone":"America/Toronto","message":"{time} {date} {timezone} {user}"}`, ""},
		{"queue.join", Queue(queueDeps(&fakeQueue{open: true})), "!join", "", ""},
		{"queue.next", Queue(queueDeps(&fakeQueue{open: true, line: []string{"alice"}})), "!queue next", "", "moderator"},
	}
	for _, tc := range cases {
		t.Run(tc.ns, func(t *testing.T) {
			c := withConfig(chatCtx("42", "alice", tc.badge), tc.config)
			out := runChat(t, tc.m, c, tc.text)
			require.Len(t, out, 1)
			assertResolved(t, tc.ns, out[0].Text)
		})
	}
}

func TestReplyTokensResolveInGameReplies(t *testing.T) {
	fortniteStats := fortniteStatsReply("lifetime")
	cases := []struct {
		ns string
		gossipCase
	}{
		{"urchin.daily", gossipCase{chat: urchinChat("!daily"), replies: map[string]any{"urchin.daily": gossiprpc.UrchinSessionReply{
			Player: "Techno", Wins: 5, Losses: 2, FinalKills: 21, FinalDeaths: 3, BedsBroken: 9, GamesPlayed: 8, Levels: 1,
		}}}},
		{"urchin.stats", gossipCase{chat: urchinChat("!bwstats"), replies: map[string]any{"hypixel.stats": gossiprpc.HypixelStatsReply{
			Player: "Techno", Stars: 402, Wins: 1000, Losses: 100, FinalKills: 5000, FinalDeaths: 500, BedsBroken: 2000,
		}}}},
		{"urchin.sniper", gossipCase{chat: urchinChat("!sniper"), replies: map[string]any{"urchin.sniper": gossiprpc.UrchinSniperReply{
			Player: "Techno", Score: 7.5, Mode: "warn", TagCount: 1,
		}}}},
		{"urchin.tags", gossipCase{chat: urchinChat("!tag"), replies: map[string]any{"urchin.tags": gossiprpc.UrchinTagsReply{
			Player: "Techno", Tags: []gossiprpc.UrchinTag{{Type: "blatant_cheater", AddedOn: 1, Reason: "bhop"}},
		}}}},
		{"mcsr.elo", gossipCase{chat: mcsrChat("!elo", ""), replies: map[string]any{"mcsr.user": gossiprpc.McsrUserReply{
			Nickname: "Feinberg", Elo: 1650, Rank: 12, Wins: 40, Loses: 20, Country: "us",
		}}}},
		{"mcsr.session", gossipCase{chat: mcsrChat("!session", ""), replies: map[string]any{"mcsr.session": gossiprpc.McsrSessionReply{
			Nickname: "Feinberg", Elo: 1660, EloChange: 24, Wins: 3, Loses: 1, Played: 4, HasSnapshot: true,
		}}}},
		{"mcsr.lastmatch", gossipCase{chat: mcsrChat("!lastmatch", ""), replies: map[string]any{"mcsr.last_match": gossiprpc.McsrLastMatchReply{
			Player: "Feinberg", Opponent: "lowk3y_", Result: "win", Time: "11:03.135", Seed: "Desert Temple", Structure: "Treasure", EloChange: 21, AgoSeconds: 120,
		}}}},
		{"mcsr.record", gossipCase{chat: mcsrChat("!record lowk3y_", ""), replies: map[string]any{"mcsr.versus": gossiprpc.McsrRecordReply{
			PlayerA: "Feinberg", PlayerB: "lowk3y_", WinsA: 20, WinsB: 14, Played: 34,
		}}}},
		{"mcsr.lb", gossipCase{chat: mcsrChat("!lb", ""), replies: map[string]any{"mcsr.leaderboard": gossiprpc.McsrLeaderboardReply{
			Entries: []gossiprpc.McsrLeaderboardEntry{{Rank: 1, Name: "Feinberg", Value: "2464"}},
		}}}},
		{"mcsr.race", gossipCase{chat: mcsrChat("!race", ""), replies: map[string]any{"mcsr.weekly_race": gossiprpc.McsrWeeklyRaceReply{
			LeaderName: "gharfyy", LeaderTime: "2:27.374", Player: "Feinberg", PlayerTime: "2:40.000", PlayerRank: 2, HasPlayer: true,
		}}}},
		{"mcsr.pb", gossipCase{chat: mcsrChat("!pb ranked", ""), replies: map[string]any{"mcsr.user": gossiprpc.McsrUserReply{Nickname: "Feinberg", BestTimeMS: 400000}}}},
		{"mcsr.pb", gossipCase{chat: mcsrChat("!pb daily", ""), replies: map[string]any{"paceman.personal_best": gossiprpc.PacemanPersonalBestReply{
			Player: "Feinberg", Window: "daily", Time: "6:40.123",
		}}}},
		{"mcsr.pace", gossipCase{chat: mcsrChat("!pace", ""), replies: map[string]any{"paceman.session": gossiprpc.PacemanSessionReply{
			Player: "Feinberg", NetherCount: 3, Nether: "1:42", Bastion: "3:55", Fortress: "7:12", FirstStructure: "3:55", SecondStructure: "7:12",
			FirstPortal: "9:20", Stronghold: "12:05", End: "13:50", Finish: "0:00", NPH: 5.3,
		}}}},
		{"mcsr.nethers", gossipCase{chat: mcsrChat("!nethers", ""), replies: map[string]any{"paceman.nethers": gossiprpc.PacemanNethersReply{
			Player: "Feinberg", Count: 3, Avg: "1:42", NPH: 5.3,
		}}}},
		{"fortnite.stats", gossipCase{chat: fortniteChat("!fnstats", ""), replies: map[string]any{"fortnite.stats": fortniteStats}}},
		{"fortnite.store", gossipCase{chat: fortniteChat("!fnstore", ""), replies: map[string]any{"fortnite.shop": gossiprpc.FortniteShopReply{
			Date: "2026-07-09", Count: 1, Entries: []gossiprpc.FortniteShopEntry{{Name: "Peely Bundle", Price: 2800}},
		}}}},
	}
	for _, tc := range cases {
		t.Run(tc.ns, func(t *testing.T) {
			assertResolved(t, tc.ns, gossipText(t, tc.gossipCase))
		})
	}
}

func TestReplyTokensClipPassthrough(t *testing.T) {
	const template = "{user} clipped {clip} for {target}"
	proj := &fakeProj{modules: []projection.ModuleView{{Name: "clip", IsEnabled: true, Configs: []byte(`{"reply":"` + template + `"}`)}}}
	out := runChat(t, Clip(engine.Deps{Proj: proj, Log: zap.NewNop()}), chatCtx("42", "viewer"), "!clip x")
	require.Len(t, out, 1)
	assert.Equal(t, template, out[0].Template, "clip.go must pass the template through untouched")
	for _, name := range ReplyTokenInventory()["builtin.clip"] {
		assert.Contains(t, template, "{"+name+"}", "builtin.clip inventory name %q missing from the sample template", name)
	}
}

func TestReplyPureFamilyWorksAcrossSurfaces(t *testing.T) {
	digits := regexp.MustCompile(`\d`)
	const message = "sum={math:1+1} in {countdown:2030-01-01}"
	surfaces := []struct {
		name string
		text func(t *testing.T) string
	}{
		{"alerts reply", func(t *testing.T) string {
			out := runAlert(t, alertsDeps(nil), eventInput{"channel.follow", followJSON, `{"followMessage":"{user} joined! ` + message + `"}`})
			require.Len(t, out, 1)
			return out[0].Text
		}},
		{"chatReplier reply (queue)", func(t *testing.T) string {
			c := withConfig(chatCtx("42", "alice"), `{"joinMessage":"welcome {user}! `+message+`"}`)
			out := runChat(t, Queue(queueDeps(&fakeQueue{open: true})), c, "!join")
			require.Len(t, out, 1)
			return out[0].Text
		}},
	}
	for _, s := range surfaces {
		t.Run(s.name, func(t *testing.T) {
			text := s.text(t)
			assert.NotContains(t, text, "{math:1+1}")
			assert.NotContains(t, text, "{countdown:2030-01-01}")
			assert.Contains(t, text, "sum=2")
			assert.True(t, digits.MatchString(text), "countdown span must render some duration, got %q", text)
		})
	}
}

func TestReplyLocaleReachesPureFamily(t *testing.T) {
	cfg := `{"followMessage":"in {countdown:9999-01-01}"}`
	countdownText := func(locale string) string {
		c := eventCtx(eventInput{"channel.follow", followJSON, cfg})
		c.Locale = locale
		out := runEvent(t, Alerts(alertsDeps(nil)), c)
		require.Len(t, out, 1)
		return out[0].Text
	}
	assert.NotEqual(t, countdownText("en"), countdownText("fr"), "a FR channel's {countdown} must not render the same words as an EN one")
}

func TestReplyPalettePayloadOnDeclaredNameStaysLiteral(t *testing.T) {
	c := withConfig(chatCtx("42", "alice"), `{"joinMessage":"hi {user:x}, spot {pos}"}`)
	out := runChat(t, Queue(queueDeps(&fakeQueue{open: true})), c, "!join")
	require.Len(t, out, 1)
	assert.Equal(t, "hi {user:x}, spot 1", out[0].Text)
}
