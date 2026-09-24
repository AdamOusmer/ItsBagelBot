// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package modules

import (
	"context"
	"testing"

	"ItsBagelBot/app/twitch/sesame/engine"
	gossiprpc "ItsBagelBot/internal/domain/rpc/gossip"
	"ItsBagelBot/internal/projection"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

type resolvedCheck struct {
	ns, text string
}

func assertResolved(t *testing.T, c resolvedCheck) {
	t.Helper()
	names, ok := ReplyTokenInventory()[c.ns]
	require.True(t, ok, "no inventory entry for %q", c.ns)
	for _, name := range names {
		assert.NotContains(t, c.text, "{"+name+"}", "%s: %q left unresolved in %q", c.ns, name, c.text)
	}
}

func TestReplyTokensAlertsResolve(t *testing.T) {
	cases := []struct {
		ns string
		in alertInput
	}{
		{"alerts.follow", alertInput{event: "channel.follow", payload: followJSON}},
		{"alerts.sub", alertInput{event: "channel.subscribe", payload: subscribeJSON}},
		{"alerts.cheer", alertInput{event: "channel.cheer", payload: cheerJSON}},
		{"alerts.raid", alertInput{event: "channel.raid", payload: raidJSON}},
	}
	for _, tc := range cases {
		out := runAlert(t, tc.in)
		require.Len(t, out, 1, tc.ns)
		assertResolved(t, resolvedCheck{tc.ns, out[0].Text})
	}
}

func TestReplyTokensShoutoutResolve(t *testing.T) {
	var col collector
	h := raidHandler(t)
	require.NoError(t, h(context.Background(), raidCtx(""), col.emit))
	require.Len(t, col.out, 1)
	assertResolved(t, resolvedCheck{"shoutout.shoutout", col.out[0].Text})
}

func TestReplyTokensTimeResolve(t *testing.T) {
	out := runTime(t, `{"timezone":"America/Toronto","message":"{time} {date} {timezone} {user}"}`)
	assertResolved(t, resolvedCheck{"time.time", out.Text})
}

func TestReplyTokensQueueResolve(t *testing.T) {
	q := &fakeQueue{open: true}
	m := Queue(queueDeps(q))

	join := runQueue(t, m, "join", queueCtx("alice", ""), "")
	require.Len(t, join, 1)
	assertResolved(t, resolvedCheck{"queue.join", join[0].Text})

	next := runQueue(t, m, "queue", queueCtx("mod", "moderator"), "next")
	require.Len(t, next, 1)
	assertResolved(t, resolvedCheck{"queue.next", next[0].Text})
}

func TestReplyTokensUrchinResolve(t *testing.T) {
	cases := []struct {
		ns, endpoint, cmd string
		reply             any
	}{
		{"urchin.daily", "urchin.daily", "daily", gossiprpc.UrchinSessionReply{
			Player: "Techno", Wins: 5, Losses: 2, FinalKills: 21, FinalDeaths: 3,
			BedsBroken: 9, GamesPlayed: 8, Levels: 1,
		}},
		{"urchin.stats", "hypixel.stats", "bwstats", gossiprpc.HypixelStatsReply{
			Player: "Techno", Stars: 402, Wins: 1000, Losses: 100, FinalKills: 5000,
			FinalDeaths: 500, BedsBroken: 2000,
		}},
		{"urchin.sniper", "urchin.sniper", "sniper", gossiprpc.UrchinSniperReply{
			Player: "Techno", Score: 7.5, Mode: "warn", TagCount: 1,
		}},
		{"urchin.tags", "urchin.tags", "tag", gossiprpc.UrchinTagsReply{
			Player: "Techno", Tags: []gossiprpc.UrchinTag{{Type: "blatant_cheater", AddedOn: 1, Reason: "bhop"}},
		}},
	}
	for _, tc := range cases {
		gw := &fakeGossip{replies: map[string]any{tc.endpoint: tc.reply}}
		cmd := urchinCmd(t, gw, tc.cmd)
		var col collector
		require.NoError(t, cmd.Run(context.Background(), urchinCtx(""), "", col.emit))
		require.Len(t, col.out, 1, tc.ns)
		assertResolved(t, resolvedCheck{tc.ns, col.out[0].Text})
	}
}

func TestReplyTokensMcsrAccountResolve(t *testing.T) {
	cases := []struct {
		ns, endpoint, cmd, args string
		reply                   any
	}{
		{"mcsr.elo", "mcsr.user", "elo", "", gossiprpc.McsrUserReply{
			Nickname: "Feinberg", Elo: 1650, Rank: 12, Wins: 40, Loses: 20, Country: "us",
		}},
		{"mcsr.session", "mcsr.session", "session", "", gossiprpc.McsrSessionReply{
			Nickname: "Feinberg", Elo: 1660, EloChange: 24, Wins: 3, Loses: 1, Played: 4, HasSnapshot: true,
		}},
		{"mcsr.lastmatch", "mcsr.last_match", "lastmatch", "", gossiprpc.McsrLastMatchReply{
			Player: "Feinberg", Opponent: "lowk3y_", Result: "win", Time: "11:03.135",
			Seed: "Desert Temple", Structure: "Treasure", EloChange: 21, AgoSeconds: 120,
		}},
		{"mcsr.record", "mcsr.versus", "record", "lowk3y_", gossiprpc.McsrRecordReply{
			PlayerA: "Feinberg", PlayerB: "lowk3y_", WinsA: 20, WinsB: 14, Played: 34,
		}},
	}
	for _, tc := range cases {
		gw := &fakeGossip{replies: map[string]any{tc.endpoint: tc.reply}}
		col := runMcsrCmd(t, gw, mcsrCmdCall{tc.cmd, "", tc.args})
		require.Len(t, col.out, 1, tc.ns)
		assertResolved(t, resolvedCheck{tc.ns, col.out[0].Text})
	}
}

func TestReplyTokensMcsrBoardResolve(t *testing.T) {
	cases := []struct {
		ns, endpoint, cmd, args string
		reply                   any
	}{
		{"mcsr.lb", "mcsr.leaderboard", "lb", "", gossiprpc.McsrLeaderboardReply{
			Entries: []gossiprpc.McsrLeaderboardEntry{{Rank: 1, Name: "Feinberg", Value: "2464"}},
		}},
		{"mcsr.race", "mcsr.weekly_race", "race", "", gossiprpc.McsrWeeklyRaceReply{
			LeaderName: "gharfyy", LeaderTime: "2:27.374", Player: "Feinberg",
			PlayerTime: "2:40.000", PlayerRank: 2, HasPlayer: true,
		}},
		{"mcsr.pb", "mcsr.user", "pb", "ranked", gossiprpc.McsrUserReply{Nickname: "Feinberg", BestTimeMS: 400000}},
		{"mcsr.pb", "paceman.personal_best", "pb", "daily", gossiprpc.PacemanPersonalBestReply{
			Player: "Feinberg", Window: "daily", Time: "6:40.123",
		}},
	}
	for _, tc := range cases {
		gw := &fakeGossip{replies: map[string]any{tc.endpoint: tc.reply}}
		col := runMcsrCmd(t, gw, mcsrCmdCall{tc.cmd, "", tc.args})
		require.Len(t, col.out, 1, tc.ns)
		assertResolved(t, resolvedCheck{tc.ns, col.out[0].Text})
	}
}

func TestReplyTokensMcsrPaceResolve(t *testing.T) {
	cases := []struct {
		ns, endpoint, cmd string
		reply             any
	}{
		{"mcsr.pace", "paceman.session", "pace", gossiprpc.PacemanSessionReply{
			Player: "Feinberg", NetherCount: 3, Nether: "1:42", Bastion: "3:55", Fortress: "7:12",
			FirstStructure: "3:55", SecondStructure: "7:12", FirstPortal: "9:20",
			Stronghold: "12:05", End: "13:50", Finish: "0:00", NPH: 5.3,
		}},
		{"mcsr.nethers", "paceman.nethers", "nethers", gossiprpc.PacemanNethersReply{
			Player: "Feinberg", Count: 3, Avg: "1:42", NPH: 5.3,
		}},
	}
	for _, tc := range cases {
		gw := &fakeGossip{replies: map[string]any{tc.endpoint: tc.reply}}
		col := runMcsrCmd(t, gw, mcsrCmdCall{tc.cmd, "", ""})
		require.Len(t, col.out, 1, tc.ns)
		assertResolved(t, resolvedCheck{tc.ns, col.out[0].Text})
	}
}

func TestReplyTokensFortniteResolve(t *testing.T) {
	stats := gossiprpc.FortniteStatsReply{
		Player: "Ninja", Window: "lifetime",
		Overall: gossiprpc.FortniteModeStats{Wins: 301, Matches: 6232, Kills: 21679, KD: 3.66, WinRate: 4.83},
		Solo:    gossiprpc.FortniteModeStats{Wins: 120, Matches: 2400, KD: 3.2},
		Duo:     gossiprpc.FortniteModeStats{Wins: 90, Matches: 1900, KD: 3.8},
		Squad:   gossiprpc.FortniteModeStats{Wins: 91, Matches: 1932, KD: 4.1},
	}
	shop := gossiprpc.FortniteShopReply{
		Date: "2026-07-09", Count: 1,
		Entries: []gossiprpc.FortniteShopEntry{{Name: "Peely Bundle", Price: 2800}},
	}
	cases := []struct {
		ns, endpoint, cmd string
		reply             any
	}{
		{"fortnite.stats", "fortnite.stats", "fnstats", stats},
		{"fortnite.store", "fortnite.shop", "fnstore", shop},
	}
	for _, tc := range cases {
		gw := &fakeGossip{replies: map[string]any{tc.endpoint: tc.reply}}
		cmd := fortniteCmd(t, gw, tc.cmd)
		var col collector
		require.NoError(t, cmd.Run(context.Background(), urchinCtx(""), "", col.emit))
		require.Len(t, col.out, 1, tc.ns)
		assertResolved(t, resolvedCheck{tc.ns, col.out[0].Text})
	}
}

func TestReplyTokensClipPassthrough(t *testing.T) {
	tmpl := "{user} clipped {clip} for {target}"
	reader := clipReader{modules: []projection.ModuleView{
		{Name: "clip", IsEnabled: true, Configs: []byte(`{"reply":"` + tmpl + `"}`)},
	}}
	cmd := clipCommand(t, engine.Deps{Proj: reader, Log: zap.NewNop()})
	var col collector
	require.NoError(t, cmd.Run(context.Background(), clipCtx(), "x", col.emit))
	require.Len(t, col.out, 1)
	assert.Equal(t, tmpl, col.out[0].Template, "clip.go must pass the template through untouched")
	for _, name := range ReplyTokenInventory()["builtin.clip"] {
		assert.Contains(t, tmpl, "{"+name+"}", "builtin.clip inventory name %q missing from the sample template", name)
	}
}
