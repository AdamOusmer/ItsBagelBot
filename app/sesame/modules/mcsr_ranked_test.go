// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package modules

import (
	"context"
	"testing"

	"ItsBagelBot/app/sesame/module"
	gossiprpc "ItsBagelBot/internal/domain/rpc/gossip"
	"ItsBagelBot/pkg/bus"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// This file holds the MCSR Ranked command tests: !elo, !session, !lastmatch,
// !record, !lb, !race and !pb. Shared fixtures (mcsrModule, mcsrCtx,
// runMcsrCmd) and the module-wiring / arg-parsing tests live in
// mcsr_test.go.

func TestMcsrEloDefaultTemplate(t *testing.T) {
	gw := &fakeGossip{replies: map[string]any{
		"mcsr.user": gossiprpc.McsrUserReply{Nickname: "Feinberg", Elo: 1650, Rank: 12, Wins: 40, Loses: 20},
	}}
	m := mcsrModule(gw)
	assert.Equal(t, "mcsr", m.Name)
	assert.Equal(t, module.KindOptIn, m.Kind)
	cmd := findCmd(t, m, "elo")

	var col collector
	require.NoError(t, cmd.Run(context.Background(), mcsrCtx(""), "", col.emit))
	require.Len(t, col.out, 1)
	assert.Equal(t, "Feinberg: 1650 elo · rank #12 · 40W 20L this season", col.out[0].Text)
}

func TestMcsrEloUnrated(t *testing.T) {
	gw := &fakeGossip{replies: map[string]any{
		"mcsr.user": gossiprpc.McsrUserReply{Nickname: "Newbie", Elo: -1, Rank: -1},
	}}
	col := runMcsrCmd(t, gw, mcsrCmdCall{"elo", "", ""})
	require.Len(t, col.out, 1)
	assert.Contains(t, col.out[0].Text, "unrated elo")
	assert.Contains(t, col.out[0].Text, "#—")
}

// TestMcsrSessionDrawsFillTheGap pins the derived {draws} token: MCSR counts
// matches that ended with no winner in playedMatches but in neither wins nor
// loses, so 3W 4L in 8 matches is upstream-correct and needs the 1D to read
// that way. See mcsrWinLossTokens for the measurement.
func TestMcsrSessionDrawsFillTheGap(t *testing.T) {
	gw := &fakeGossip{replies: map[string]any{
		"mcsr.session": gossiprpc.McsrSessionReply{
			Nickname: "LawnMobius", Elo: 1568, EloChange: -13, Wins: 3, Loses: 4, Played: 8, HasSnapshot: true,
		},
	}}
	col := runMcsrCmd(t, gw, mcsrCmdCall{"session", `{"account":"LawnMobius"}`, ""})
	require.Len(t, col.out, 1)
	assert.Equal(t, "LawnMobius this stream: -13 elo (1568 now) · 3W 4L 1D in 8 matches", col.out[0].Text)
}

// TestMcsrSessionDrawsNeverNegative covers a season rollover under a live
// snapshot: the live counters reset below the baseline, so the subtraction
// would go negative and render "-2D".
func TestMcsrSessionDrawsNeverNegative(t *testing.T) {
	assert.Equal(t, "0", mcsrWinLossTokens(3, 1, 2)["draws"])
}

// TestMcsrSessionTemplateUpgrade covers the three stored shapes: blank falls
// through to the current default, a config holding the pre-{draws} default
// verbatim is upgraded to it, and an edited template — even one byte off — is
// served exactly as written.
func TestMcsrSessionTemplateUpgrade(t *testing.T) {
	assert.Equal(t, defaultMcsrSessionTemplate, mcsrSessionTemplate(""))
	assert.Equal(t, defaultMcsrSessionTemplate, mcsrSessionTemplate(legacyMcsrSessionTemplate))
	assert.Equal(t, "{wins}-{losses}", mcsrSessionTemplate("{wins}-{losses}"))
	assert.Equal(t, legacyMcsrSessionTemplate+"!", mcsrSessionTemplate(legacyMcsrSessionTemplate+"!"))
}

func TestMcsrSessionWithSnapshot(t *testing.T) {
	gw := &fakeGossip{replies: map[string]any{
		"mcsr.session": gossiprpc.McsrSessionReply{
			Nickname: "Feinberg", Elo: 1660, EloChange: 24, Wins: 3, Loses: 1, Played: 4, HasSnapshot: true,
		},
	}}
	col := runMcsrCmd(t, gw, mcsrCmdCall{"session", `{"account":"Feinberg"}`, ""})
	require.Len(t, col.out, 1)
	assert.Equal(t, "Feinberg this stream: +24 elo (1660 now) · 3W 1L 0D in 4 matches", col.out[0].Text)

	// The session request is scoped to this channel.
	assert.Equal(t, "2", gw.lastCall(t).req.ChannelID)
	assert.Equal(t, "Feinberg", gw.lastCall(t).req.Account)
}

// !session ignores a typed player argument so a viewer cannot retarget (and
// clobber) the streamer's per-channel baseline; it always uses the linked
// account.
func TestMcsrSessionIgnoresArgument(t *testing.T) {
	gw := &fakeGossip{replies: map[string]any{
		"mcsr.session": gossiprpc.McsrSessionReply{Nickname: "Feinberg", HasSnapshot: true},
	}}
	runMcsrCmd(t, gw, mcsrCmdCall{"session", `{"account":"Feinberg"}`, "SomeoneElse"})
	assert.Equal(t, "Feinberg", gw.lastCall(t).req.Account)
}

func TestMcsrSessionWithoutSnapshot(t *testing.T) {
	gw := &fakeGossip{replies: map[string]any{
		"mcsr.session": gossiprpc.McsrSessionReply{Nickname: "Feinberg", Elo: 1650, HasSnapshot: false},
	}}
	col := runMcsrCmd(t, gw, mcsrCmdCall{"session", "", ""})
	require.Len(t, col.out, 1)
	assert.Contains(t, col.out[0].Text, "session tracking just started")
}

func TestMcsrSessionToggleOff(t *testing.T) {
	gw := &fakeGossip{replies: map[string]any{"mcsr.session": gossiprpc.McsrSessionReply{}}}
	col := runMcsrCmd(t, gw, mcsrCmdCall{"session", `{"sessionEnabled":"off"}`, ""})
	assert.Empty(t, col.out)
	assert.Empty(t, gw.calls)
}

func TestMcsrLastMatchDefaultTemplate(t *testing.T) {
	gw := &fakeGossip{replies: map[string]any{
		"mcsr.last_match": gossiprpc.McsrLastMatchReply{
			Player: "Feinberg", Opponent: "lowk3y_", Result: "win", Time: "11:03.135",
			Seed: "Desert Temple", Structure: "Treasure", EloChange: 21, AgoSeconds: 125,
		},
	}}
	col := runMcsrCmd(t, gw, mcsrCmdCall{"lastmatch", "", ""})
	require.Len(t, col.out, 1)
	assert.Equal(t, "Feinberg vs lowk3y_: won · 11:03.135 · Desert Temple Treasure · +21 elo · 2m ago", col.out[0].Text)
}

// A forfeit must read as a forfeit, not a clean win: the module appends the
// translated "(forfeit)" suffix and the missing time renders as a dash
// instead of an empty gap in the template.
func TestMcsrLastMatchForfeit(t *testing.T) {
	gw := &fakeGossip{replies: map[string]any{
		"mcsr.last_match": gossiprpc.McsrLastMatchReply{
			Player: "Feinberg", Opponent: "lowk3y_", Result: "loss", Forfeited: true,
		},
	}}
	col := runMcsrCmd(t, gw, mcsrCmdCall{"lastmatch", "", ""})
	require.Len(t, col.out, 1)
	assert.Contains(t, col.out[0].Text, "lost (forfeit)")
	assert.Contains(t, col.out[0].Text, "—", "no completion time renders as a dash")
}

func TestMcsrLastMatchDecayed(t *testing.T) {
	gw := &fakeGossip{replies: map[string]any{
		"mcsr.last_match": gossiprpc.McsrLastMatchReply{Player: "Feinberg", Opponent: "lowk3y_", Result: "win", Decayed: true},
	}}
	col := runMcsrCmd(t, gw, mcsrCmdCall{"lastmatch", "", ""})
	require.Len(t, col.out, 1)
	assert.Contains(t, col.out[0].Text, "won (decay)")
}

func TestMcsrLastMatchEmpty(t *testing.T) {
	gw := &fakeGossip{replies: map[string]any{
		"mcsr.last_match": gossiprpc.McsrLastMatchReply{Player: "Newbie", Empty: true},
	}}
	col := runMcsrCmd(t, gw, mcsrCmdCall{"lastmatch", "", ""})
	require.Len(t, col.out, 1)
	assert.Contains(t, col.out[0].Text, "no matches found yet")
}

func TestMcsrLastMatchToggleOff(t *testing.T) {
	gw := &fakeGossip{replies: map[string]any{"mcsr.last_match": gossiprpc.McsrLastMatchReply{}}}
	col := runMcsrCmd(t, gw, mcsrCmdCall{"lastmatch", `{"lastMatchEnabled":"off"}`, ""})
	assert.Empty(t, col.out)
	assert.Empty(t, gw.calls)
}

func TestMcsrLastMatchSeasonToken(t *testing.T) {
	gw := &fakeGossip{replies: map[string]any{
		"mcsr.last_match": gossiprpc.McsrLastMatchReply{Player: "Feinberg", Empty: true},
	}}
	runMcsrCmd(t, gw, mcsrCmdCall{"lastmatch", "", "Feinberg season:11"})
	call := gw.lastCall(t)
	assert.Equal(t, "Feinberg", call.req.Account, "the season token must not leak into the player name")
	assert.Equal(t, 11, call.req.Season)
}

func TestMcsrRecordDefaultTemplate(t *testing.T) {
	gw := &fakeGossip{replies: map[string]any{
		"mcsr.versus": gossiprpc.McsrRecordReply{PlayerA: "Feinberg", PlayerB: "lowk3y_", WinsA: 20, WinsB: 14, Played: 34},
	}}
	col := runMcsrCmd(t, gw, mcsrCmdCall{"record", "", "Feinberg lowk3y_"})
	require.Len(t, col.out, 1)
	assert.Equal(t, "Feinberg 20 - 14 lowk3y_ · 34 played", col.out[0].Text)
	assert.Equal(t, "Feinberg", gw.lastCall(t).req.Account)
	assert.Equal(t, "lowk3y_", gw.lastCall(t).req.AccountB)
}

// Only one typed username compares it against the module's linked account.
func TestMcsrRecordSingleArgUsesLinkedAccount(t *testing.T) {
	gw := &fakeGossip{replies: map[string]any{
		"mcsr.versus": gossiprpc.McsrRecordReply{PlayerA: "Feinberg", PlayerB: "lowk3y_"},
	}}
	runMcsrCmd(t, gw, mcsrCmdCall{"record", `{"account":"Feinberg"}`, "lowk3y_"})
	call := gw.lastCall(t)
	assert.Equal(t, "Feinberg", call.req.Account)
	assert.Equal(t, "lowk3y_", call.req.AccountB)
}

func TestMcsrRecordNoArgsShowsUsage(t *testing.T) {
	gw := &fakeGossip{replies: map[string]any{"mcsr.versus": gossiprpc.McsrRecordReply{}}}
	col := runMcsrCmd(t, gw, mcsrCmdCall{"record", "", ""})
	require.Len(t, col.out, 1)
	assert.Contains(t, col.out[0].Text, "Usage")
	assert.Empty(t, gw.calls, "no upstream call without at least one typed player")
}

func TestMcsrRecordToggleOff(t *testing.T) {
	gw := &fakeGossip{replies: map[string]any{"mcsr.versus": gossiprpc.McsrRecordReply{}}}
	col := runMcsrCmd(t, gw, mcsrCmdCall{"record", `{"recordEnabled":"off"}`, "a b"})
	assert.Empty(t, col.out)
	assert.Empty(t, gw.calls)
}

func TestMcsrLbDefaultTemplateElo(t *testing.T) {
	gw := &fakeGossip{replies: map[string]any{
		"mcsr.leaderboard": gossiprpc.McsrLeaderboardReply{Board: "elo", Entries: []gossiprpc.McsrLeaderboardEntry{
			{Rank: 1, Name: "A", Value: "2400"},
			{Rank: 2, Name: "B", Value: "2380"},
		}},
	}}
	col := runMcsrCmd(t, gw, mcsrCmdCall{"lb", "", ""})
	require.Len(t, col.out, 1)
	assert.Equal(t, "Elo: #1 A 2400 · #2 B 2380", col.out[0].Text)
	assert.Equal(t, "", gw.lastCall(t).req.Board)
}

func TestMcsrLbPhasePredictedAndCountry(t *testing.T) {
	gw := &fakeGossip{replies: map[string]any{
		"mcsr.leaderboard": gossiprpc.McsrLeaderboardReply{Board: "phase", Entries: []gossiprpc.McsrLeaderboardEntry{{Rank: 1, Name: "A", Value: "80"}}},
	}}
	col := runMcsrCmd(t, gw, mcsrCmdCall{"lb", "", "phase predicted country:us"})
	require.Len(t, col.out, 1)
	assert.Equal(t, "Phase: #1 A 80", col.out[0].Text)
	call := gw.lastCall(t)
	assert.Equal(t, "phase", call.req.Board)
	assert.True(t, call.req.Predicted)
	assert.Equal(t, "us", call.req.Country)
}

func TestMcsrLbEmpty(t *testing.T) {
	gw := &fakeGossip{replies: map[string]any{
		"mcsr.leaderboard": gossiprpc.McsrLeaderboardReply{Board: "record", Empty: true},
	}}
	col := runMcsrCmd(t, gw, mcsrCmdCall{"lb", "", "record"})
	require.Len(t, col.out, 1)
	assert.Contains(t, col.out[0].Text, "nobody on this leaderboard yet")
}

func TestMcsrLbToggleOff(t *testing.T) {
	gw := &fakeGossip{replies: map[string]any{"mcsr.leaderboard": gossiprpc.McsrLeaderboardReply{}}}
	col := runMcsrCmd(t, gw, mcsrCmdCall{"lb", `{"lbEnabled":"off"}`, ""})
	assert.Empty(t, col.out)
	assert.Empty(t, gw.calls)
}

func TestMcsrLbSeasonToken(t *testing.T) {
	gw := &fakeGossip{replies: map[string]any{
		"mcsr.leaderboard": gossiprpc.McsrLeaderboardReply{Board: "elo", Empty: true},
	}}
	runMcsrCmd(t, gw, mcsrCmdCall{"lb", "", "season:11"})
	assert.Equal(t, 11, gw.lastCall(t).req.Season)
}

func TestMcsrRaceDefaultTemplate(t *testing.T) {
	gw := &fakeGossip{replies: map[string]any{
		"mcsr.weekly_race": gossiprpc.McsrWeeklyRaceReply{
			LeaderName: "gharfyy", LeaderTime: "2:27.374",
			Player: "Feinberg", PlayerTime: "2:40.000", PlayerRank: 2, HasPlayer: true,
		},
	}}
	col := runMcsrCmd(t, gw, mcsrCmdCall{"race", "", ""})
	require.Len(t, col.out, 1)
	assert.Equal(t, "#1 gharfyy (2:27.374) · Feinberg: 2:40.000 (#2)", col.out[0].Text)
}

func TestMcsrRaceNoPlayerTime(t *testing.T) {
	gw := &fakeGossip{replies: map[string]any{
		"mcsr.weekly_race": gossiprpc.McsrWeeklyRaceReply{LeaderName: "gharfyy", LeaderTime: "2:27.374", Player: "Newbie", HasPlayer: false},
	}}
	col := runMcsrCmd(t, gw, mcsrCmdCall{"race", "", ""})
	require.Len(t, col.out, 1)
	assert.Contains(t, col.out[0].Text, "#1 gharfyy (2:27.374)")
	assert.Contains(t, col.out[0].Text, "no time in this week's race yet")
}

func TestMcsrRaceEmpty(t *testing.T) {
	gw := &fakeGossip{replies: map[string]any{"mcsr.weekly_race": gossiprpc.McsrWeeklyRaceReply{Empty: true}}}
	col := runMcsrCmd(t, gw, mcsrCmdCall{"race", "", ""})
	require.Len(t, col.out, 1)
	assert.Contains(t, col.out[0].Text, "no times submitted for this week's race yet")
}

func TestMcsrRaceToggleOff(t *testing.T) {
	gw := &fakeGossip{replies: map[string]any{"mcsr.weekly_race": gossiprpc.McsrWeeklyRaceReply{}}}
	col := runMcsrCmd(t, gw, mcsrCmdCall{"race", `{"raceEnabled":"off"}`, ""})
	assert.Empty(t, col.out)
	assert.Empty(t, gw.calls)
}

func TestMcsrEloNoSeasonTokenUnchanged(t *testing.T) {
	gw := &fakeGossip{replies: map[string]any{
		"mcsr.user": gossiprpc.McsrUserReply{Nickname: "Feinberg", Elo: 1650, Rank: 12},
	}}
	runMcsrCmd(t, gw, mcsrCmdCall{"elo", "", ""})
	assert.Zero(t, gw.lastCall(t).req.Season)
}

func TestMcsrEloSeasonToken(t *testing.T) {
	gw := &fakeGossip{replies: map[string]any{
		"mcsr.user": gossiprpc.McsrUserReply{Nickname: "Feinberg", Elo: 1650, Rank: 12},
	}}
	runMcsrCmd(t, gw, mcsrCmdCall{"elo", "", "Feinberg season:5"})
	call := gw.lastCall(t)
	assert.Equal(t, "Feinberg", call.req.Account)
	assert.Equal(t, 5, call.req.Season)
}

// --- !pb ---------------------------------------------------------------------------

func TestMcsrPbBareIsAllTime(t *testing.T) {
	gw := &fakeGossip{replies: map[string]any{
		"paceman.personal_best": gossiprpc.PacemanPersonalBestReply{Player: "Feinberg", Window: "all-time", Time: "6:10.012"},
	}}
	col := runMcsrCmd(t, gw, mcsrCmdCall{"pb", `{"account":"Feinberg"}`, ""})
	require.Len(t, col.out, 1)
	assert.Equal(t, "Feinberg: 6:10.012 (all-time PB)", col.out[0].Text)

	call := gw.lastCall(t)
	assert.Equal(t, "paceman", call.provider)
	assert.Equal(t, "personal_best", call.endpoint)
	assert.Equal(t, "", call.req.TimeWindow, "no window typed must mean all-time, not an unset request")
}

func TestMcsrPbWindowArg(t *testing.T) {
	cases := []struct{ window string }{{"daily"}, {"weekly"}, {"monthly"}}
	for _, tc := range cases {
		t.Run(tc.window, func(t *testing.T) {
			gw := &fakeGossip{replies: map[string]any{
				"paceman.personal_best": gossiprpc.PacemanPersonalBestReply{Player: "Feinberg", Window: tc.window, Time: "6:40.123"},
			}}
			col := runMcsrCmd(t, gw, mcsrCmdCall{"pb", `{"account":"Feinberg"}`, tc.window})
			require.Len(t, col.out, 1)
			assert.Equal(t, "Feinberg: 6:40.123 ("+tc.window+" PB)", col.out[0].Text)
			assert.Equal(t, tc.window, gw.lastCall(t).req.TimeWindow)
		})
	}
}

// A bare typed name with no window keyword resolves as the account, not a
// window — "!pb Feinberg" is the all-time form for that player, same as
// !elo/!pace's argument handling.
func TestMcsrPbBareName(t *testing.T) {
	gw := &fakeGossip{replies: map[string]any{
		"paceman.personal_best": gossiprpc.PacemanPersonalBestReply{Player: "lowk3y_", Window: "all-time", Time: "5:59.000"},
	}}
	runMcsrCmd(t, gw, mcsrCmdCall{"pb", `{"account":"Feinberg"}`, "lowk3y_"})
	call := gw.lastCall(t)
	assert.Equal(t, "lowk3y_", call.req.Account)
	assert.Equal(t, "", call.req.TimeWindow)
}

func TestMcsrPbWindowAndName(t *testing.T) {
	gw := &fakeGossip{replies: map[string]any{
		"paceman.personal_best": gossiprpc.PacemanPersonalBestReply{Player: "lowk3y_", Window: "weekly", Time: "6:01.500"},
	}}
	runMcsrCmd(t, gw, mcsrCmdCall{"pb", `{"account":"Feinberg"}`, "weekly lowk3y_"})
	call := gw.lastCall(t)
	assert.Equal(t, "lowk3y_", call.req.Account)
	assert.Equal(t, "weekly", call.req.TimeWindow)
}

// No personal best in the requested window is a normal PaceMan answer, not
// an error: it must render the translated plain line, never a zero time.
func TestMcsrPbNoneInWindow(t *testing.T) {
	gw := &fakeGossip{replies: map[string]any{
		"paceman.personal_best": gossiprpc.PacemanPersonalBestReply{Player: "Newbie", Window: "daily", Empty: true},
	}}
	col := runMcsrCmd(t, gw, mcsrCmdCall{"pb", `{"account":"Newbie"}`, "daily"})
	require.Len(t, col.out, 1)
	assert.Equal(t, "Newbie: no personal best yet (daily)", col.out[0].Text)
}

func TestMcsrPbRanked(t *testing.T) {
	gw := &fakeGossip{replies: map[string]any{
		"mcsr.user": gossiprpc.McsrUserReply{Nickname: "Feinberg", Elo: 1650, BestTimeMS: 595036},
	}}
	col := runMcsrCmd(t, gw, mcsrCmdCall{"pb", `{"account":"Feinberg"}`, "ranked"})
	require.Len(t, col.out, 1)
	assert.Equal(t, "Feinberg: 9:55.036 (ranked PB)", col.out[0].Text)

	call := gw.lastCall(t)
	assert.Equal(t, "mcsr", call.provider)
	assert.Equal(t, "user", call.endpoint)
}

// An unrated player never got a season best recorded upstream (BestTimeMS
// stays 0), same "no personal best" line as an empty PaceMan window.
func TestMcsrPbRankedUnrated(t *testing.T) {
	gw := &fakeGossip{replies: map[string]any{
		"mcsr.user": gossiprpc.McsrUserReply{Nickname: "Newbie", Elo: -1, BestTimeMS: 0},
	}}
	col := runMcsrCmd(t, gw, mcsrCmdCall{"pb", `{"account":"Newbie"}`, "ranked"})
	require.Len(t, col.out, 1)
	assert.Equal(t, "Newbie: no personal best yet (ranked)", col.out[0].Text)
}

func TestMcsrPbToggleOff(t *testing.T) {
	gw := &fakeGossip{replies: map[string]any{
		"paceman.personal_best": gossiprpc.PacemanPersonalBestReply{Player: "Feinberg", Time: "6:10.012"},
	}}
	col := runMcsrCmd(t, gw, mcsrCmdCall{"pb", `{"pbEnabled":"off"}`, ""})
	assert.Empty(t, col.out)
	assert.Empty(t, gw.calls)
}

func TestMcsrPbUpstream4xxChatsBack(t *testing.T) {
	gw := &fakeGossip{err: bus.RPCReplyError{Message: "player not found"}}
	col := runMcsrCmd(t, gw, mcsrCmdCall{"pb", "", "ghostplayer"})
	require.Len(t, col.out, 1)
	assert.Equal(t, "ghostplayer: player not found", col.out[0].Text)
}
