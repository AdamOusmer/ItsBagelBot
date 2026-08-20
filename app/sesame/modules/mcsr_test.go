// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package modules

import (
	"context"
	"testing"
	"time"

	"ItsBagelBot/app/sesame/engine"
	"ItsBagelBot/app/sesame/module"
	"ItsBagelBot/internal/domain/event/lane"
	gossiprpc "ItsBagelBot/internal/domain/rpc/gossip"
	"ItsBagelBot/pkg/bus"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func mcsrModule(gw engine.GossipCaller) module.Module {
	return Mcsr(engine.Deps{Gossip: gw, Log: zap.NewNop()})
}

func mcsrCtx(config string) *module.Context {
	c := urchinCtx(config) // same envelope shape
	return c
}

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
	cmd := findCmd(t, mcsrModule(gw), "elo")

	var col collector
	require.NoError(t, cmd.Run(context.Background(), mcsrCtx(""), "", col.emit))
	require.Len(t, col.out, 1)
	assert.Contains(t, col.out[0].Text, "unrated elo")
	assert.Contains(t, col.out[0].Text, "#—")
}

func TestMcsrSessionWithSnapshot(t *testing.T) {
	gw := &fakeGossip{replies: map[string]any{
		"mcsr.session": gossiprpc.McsrSessionReply{
			Nickname: "Feinberg", Elo: 1660, EloChange: 24, Wins: 3, Loses: 1, Played: 4, HasSnapshot: true,
		},
	}}
	cmd := findCmd(t, mcsrModule(gw), "session")

	var col collector
	require.NoError(t, cmd.Run(context.Background(), mcsrCtx(`{"account":"Feinberg"}`), "", col.emit))
	require.Len(t, col.out, 1)
	assert.Equal(t, "Feinberg this stream: +24 elo (1660 now) · 3W 1L in 4 matches", col.out[0].Text)

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
	cmd := findCmd(t, mcsrModule(gw), "session")

	var col collector
	require.NoError(t, cmd.Run(context.Background(), mcsrCtx(`{"account":"Feinberg"}`), "SomeoneElse", col.emit))
	assert.Equal(t, "Feinberg", gw.lastCall(t).req.Account)
}

func TestMcsrSessionWithoutSnapshot(t *testing.T) {
	gw := &fakeGossip{replies: map[string]any{
		"mcsr.session": gossiprpc.McsrSessionReply{Nickname: "Feinberg", Elo: 1650, HasSnapshot: false},
	}}
	cmd := findCmd(t, mcsrModule(gw), "session")

	var col collector
	require.NoError(t, cmd.Run(context.Background(), mcsrCtx(""), "", col.emit))
	require.Len(t, col.out, 1)
	assert.Contains(t, col.out[0].Text, "session tracking just started")
}

func TestMcsrSessionToggleOff(t *testing.T) {
	gw := &fakeGossip{replies: map[string]any{"mcsr.session": gossiprpc.McsrSessionReply{}}}
	cmd := findCmd(t, mcsrModule(gw), "session")

	var col collector
	require.NoError(t, cmd.Run(context.Background(), mcsrCtx(`{"sessionEnabled":"off"}`), "", col.emit))
	assert.Empty(t, col.out)
	assert.Empty(t, gw.calls)
}

func TestMcsrStreamOnlineSnapshots(t *testing.T) {
	done := make(chan struct{})
	gw := &fakeGossip{
		replies: map[string]any{"mcsr.session_start": gossiprpc.McsrSnapshotReply{Nickname: "Feinberg", Elo: 1650}},
		done:    done,
	}
	m := mcsrModule(gw)
	h := m.Events["stream.online"]
	require.NotNil(t, h, "mcsr must handle stream.online")

	c := &module.Context{
		Env: lane.Envelope{
			Type:                 "stream.online",
			BroadcasterUserID:    "2",
			BroadcasterUserLogin: "streamer",
		},
		BroadcasterID: 2,
		Log:           zap.NewNop(),
		Config:        []byte(`{"account":"Feinberg"}`),
	}
	var col collector
	require.NoError(t, h(context.Background(), c, col.emit))
	assert.Empty(t, col.out, "snapshot handler must not chat")

	// The snapshot call is fire-and-forget on its own goroutine.
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("stream.online never called gossip")
	}
	call := gw.lastCall(t)
	assert.Equal(t, "mcsr", call.provider)
	assert.Equal(t, "session_start", call.endpoint)
	assert.Equal(t, "Feinberg", call.req.Account)
	assert.Equal(t, "2", call.req.ChannelID)
}

func TestMcsrPaceDefaultTemplate(t *testing.T) {
	gw := &fakeGossip{replies: map[string]any{
		"paceman.session": gossiprpc.PacemanSessionReply{
			Player: "Feinberg", NetherCount: 3, Nether: "1:42", Bastion: "3:55",
			Fortress: "7:12", FirstPortal: "9:20", Stronghold: "12:05", End: "13:50", Finish: "0:00", NPH: 21.4,
		},
	}}
	cmd := findCmd(t, mcsrModule(gw), "pace")

	var col collector
	require.NoError(t, cmd.Run(context.Background(), mcsrCtx(""), "", col.emit))
	require.Len(t, col.out, 1)
	assert.Equal(t, "Feinberg this session: 3 nethers (avg 1:42) · bastion 3:55 · fortress 7:12 · fp 9:20 · 21.4 nph", col.out[0].Text)
}

func TestMcsrPaceEmptySession(t *testing.T) {
	gw := &fakeGossip{replies: map[string]any{
		"paceman.session": gossiprpc.PacemanSessionReply{Player: "Newbie", Empty: true},
	}}
	cmd := findCmd(t, mcsrModule(gw), "pace")

	var col collector
	require.NoError(t, cmd.Run(context.Background(), mcsrCtx(""), "", col.emit))
	require.Len(t, col.out, 1)
	assert.Contains(t, col.out[0].Text, "no pace tracked this session")
}

func TestMcsrPaceToggleOff(t *testing.T) {
	gw := &fakeGossip{replies: map[string]any{"paceman.session": gossiprpc.PacemanSessionReply{}}}
	cmd := findCmd(t, mcsrModule(gw), "pace")

	var col collector
	require.NoError(t, cmd.Run(context.Background(), mcsrCtx(`{"paceEnabled":"off"}`), "", col.emit))
	assert.Empty(t, col.out)
	assert.Empty(t, gw.calls)
}

func TestMcsrNethersDefaultTemplate(t *testing.T) {
	gw := &fakeGossip{replies: map[string]any{
		"paceman.nethers": gossiprpc.PacemanNethersReply{Player: "Feinberg", Count: 3, Avg: "1:42", NPH: 21.4},
	}}
	cmd := findCmd(t, mcsrModule(gw), "nethers")

	var col collector
	require.NoError(t, cmd.Run(context.Background(), mcsrCtx(""), "", col.emit))
	require.Len(t, col.out, 1)
	assert.Equal(t, "Feinberg: 3 nethers this session (avg 1:42) · 21.4 nph", col.out[0].Text)
}

func TestMcsrNethersEmptySession(t *testing.T) {
	gw := &fakeGossip{replies: map[string]any{
		"paceman.nethers": gossiprpc.PacemanNethersReply{Player: "Newbie", Empty: true},
	}}
	cmd := findCmd(t, mcsrModule(gw), "nethers")

	var col collector
	require.NoError(t, cmd.Run(context.Background(), mcsrCtx(""), "", col.emit))
	require.Len(t, col.out, 1)
	assert.Contains(t, col.out[0].Text, "no pace tracked this session")
}

func TestMcsrLastFortDefaultTemplate(t *testing.T) {
	gw := &fakeGossip{replies: map[string]any{
		"paceman.lastfort": gossiprpc.PacemanLastFortReply{
			Player: "Feinberg", Nether: "1:30", Bastion: "2:45", Fortress: "5:00",
			FirstPortal: "", Stronghold: "", AgoSeconds: 125,
		},
	}}
	cmd := findCmd(t, mcsrModule(gw), "lastfort")

	var col collector
	require.NoError(t, cmd.Run(context.Background(), mcsrCtx(""), "", col.emit))
	require.Len(t, col.out, 1)
	assert.Equal(t, "Feinberg last fort: nether 1:30 · bastion 2:45 · fortress 5:00 · fp — · sh — · 2m ago", col.out[0].Text)
}

func TestMcsrLastFortEmpty(t *testing.T) {
	gw := &fakeGossip{replies: map[string]any{
		"paceman.lastfort": gossiprpc.PacemanLastFortReply{Player: "Feinberg", Empty: true},
	}}
	cmd := findCmd(t, mcsrModule(gw), "lastfort")

	var col collector
	require.NoError(t, cmd.Run(context.Background(), mcsrCtx(""), "", col.emit))
	require.Len(t, col.out, 1)
	assert.Contains(t, col.out[0].Text, "no fortress pace tracked recently")
}

func TestMcsrLastFortToggleOff(t *testing.T) {
	gw := &fakeGossip{replies: map[string]any{"paceman.lastfort": gossiprpc.PacemanLastFortReply{}}}
	cmd := findCmd(t, mcsrModule(gw), "lastfort")

	var col collector
	require.NoError(t, cmd.Run(context.Background(), mcsrCtx(`{"lastFortEnabled":"off"}`), "", col.emit))
	assert.Empty(t, col.out)
	assert.Empty(t, gw.calls)
}

// !pace accepts a typed player argument, unlike !session which never does
// (its baseline is per-channel, not per-player).
func TestMcsrPaceAcceptsArgument(t *testing.T) {
	gw := &fakeGossip{replies: map[string]any{
		"paceman.session": gossiprpc.PacemanSessionReply{Player: "SomeoneElse", Empty: true},
	}}
	cmd := findCmd(t, mcsrModule(gw), "pace")

	var col collector
	require.NoError(t, cmd.Run(context.Background(), mcsrCtx(""), "SomeoneElse", col.emit))
	assert.Equal(t, "SomeoneElse", gw.lastCall(t).req.Account)
}

func TestMcsrLastMatchDefaultTemplate(t *testing.T) {
	gw := &fakeGossip{replies: map[string]any{
		"mcsr.last_match": gossiprpc.McsrLastMatchReply{
			Player: "Feinberg", Opponent: "lowk3y_", Result: "win", Time: "11:03.135",
			Seed: "Desert Temple", Structure: "Treasure", EloChange: 21, AgoSeconds: 125,
		},
	}}
	cmd := findCmd(t, mcsrModule(gw), "lastmatch")

	var col collector
	require.NoError(t, cmd.Run(context.Background(), mcsrCtx(""), "", col.emit))
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
	cmd := findCmd(t, mcsrModule(gw), "lastmatch")

	var col collector
	require.NoError(t, cmd.Run(context.Background(), mcsrCtx(""), "", col.emit))
	require.Len(t, col.out, 1)
	assert.Contains(t, col.out[0].Text, "lost (forfeit)")
	assert.Contains(t, col.out[0].Text, "—", "no completion time renders as a dash")
}

func TestMcsrLastMatchDecayed(t *testing.T) {
	gw := &fakeGossip{replies: map[string]any{
		"mcsr.last_match": gossiprpc.McsrLastMatchReply{Player: "Feinberg", Opponent: "lowk3y_", Result: "win", Decayed: true},
	}}
	cmd := findCmd(t, mcsrModule(gw), "lastmatch")

	var col collector
	require.NoError(t, cmd.Run(context.Background(), mcsrCtx(""), "", col.emit))
	require.Len(t, col.out, 1)
	assert.Contains(t, col.out[0].Text, "won (decay)")
}

func TestMcsrLastMatchEmpty(t *testing.T) {
	gw := &fakeGossip{replies: map[string]any{
		"mcsr.last_match": gossiprpc.McsrLastMatchReply{Player: "Newbie", Empty: true},
	}}
	cmd := findCmd(t, mcsrModule(gw), "lastmatch")

	var col collector
	require.NoError(t, cmd.Run(context.Background(), mcsrCtx(""), "", col.emit))
	require.Len(t, col.out, 1)
	assert.Contains(t, col.out[0].Text, "no matches found yet")
}

func TestMcsrLastMatchToggleOff(t *testing.T) {
	gw := &fakeGossip{replies: map[string]any{"mcsr.last_match": gossiprpc.McsrLastMatchReply{}}}
	cmd := findCmd(t, mcsrModule(gw), "lastmatch")

	var col collector
	require.NoError(t, cmd.Run(context.Background(), mcsrCtx(`{"lastMatchEnabled":"off"}`), "", col.emit))
	assert.Empty(t, col.out)
	assert.Empty(t, gw.calls)
}

func TestMcsrLastMatchSeasonToken(t *testing.T) {
	gw := &fakeGossip{replies: map[string]any{
		"mcsr.last_match": gossiprpc.McsrLastMatchReply{Player: "Feinberg", Empty: true},
	}}
	cmd := findCmd(t, mcsrModule(gw), "lastmatch")

	var col collector
	require.NoError(t, cmd.Run(context.Background(), mcsrCtx(""), "Feinberg season:11", col.emit))
	call := gw.lastCall(t)
	assert.Equal(t, "Feinberg", call.req.Account, "the season token must not leak into the player name")
	assert.Equal(t, 11, call.req.Season)
}

func TestMcsrRecordDefaultTemplate(t *testing.T) {
	gw := &fakeGossip{replies: map[string]any{
		"mcsr.versus": gossiprpc.McsrRecordReply{PlayerA: "Feinberg", PlayerB: "lowk3y_", WinsA: 20, WinsB: 14, Played: 34},
	}}
	cmd := findCmd(t, mcsrModule(gw), "record")

	var col collector
	require.NoError(t, cmd.Run(context.Background(), mcsrCtx(""), "Feinberg lowk3y_", col.emit))
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
	cmd := findCmd(t, mcsrModule(gw), "record")

	var col collector
	require.NoError(t, cmd.Run(context.Background(), mcsrCtx(`{"account":"Feinberg"}`), "lowk3y_", col.emit))
	call := gw.lastCall(t)
	assert.Equal(t, "Feinberg", call.req.Account)
	assert.Equal(t, "lowk3y_", call.req.AccountB)
}

func TestMcsrRecordNoArgsShowsUsage(t *testing.T) {
	gw := &fakeGossip{replies: map[string]any{"mcsr.versus": gossiprpc.McsrRecordReply{}}}
	cmd := findCmd(t, mcsrModule(gw), "record")

	var col collector
	require.NoError(t, cmd.Run(context.Background(), mcsrCtx(""), "", col.emit))
	require.Len(t, col.out, 1)
	assert.Contains(t, col.out[0].Text, "Usage")
	assert.Empty(t, gw.calls, "no upstream call without at least one typed player")
}

func TestMcsrRecordToggleOff(t *testing.T) {
	gw := &fakeGossip{replies: map[string]any{"mcsr.versus": gossiprpc.McsrRecordReply{}}}
	cmd := findCmd(t, mcsrModule(gw), "record")

	var col collector
	require.NoError(t, cmd.Run(context.Background(), mcsrCtx(`{"recordEnabled":"off"}`), "a b", col.emit))
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
	cmd := findCmd(t, mcsrModule(gw), "lb")

	var col collector
	require.NoError(t, cmd.Run(context.Background(), mcsrCtx(""), "", col.emit))
	require.Len(t, col.out, 1)
	assert.Equal(t, "Elo: #1 A 2400 · #2 B 2380", col.out[0].Text)
	assert.Equal(t, "", gw.lastCall(t).req.Board)
}

func TestMcsrLbPhasePredictedAndCountry(t *testing.T) {
	gw := &fakeGossip{replies: map[string]any{
		"mcsr.leaderboard": gossiprpc.McsrLeaderboardReply{Board: "phase", Entries: []gossiprpc.McsrLeaderboardEntry{{Rank: 1, Name: "A", Value: "80"}}},
	}}
	cmd := findCmd(t, mcsrModule(gw), "lb")

	var col collector
	require.NoError(t, cmd.Run(context.Background(), mcsrCtx(""), "phase predicted country:us", col.emit))
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
	cmd := findCmd(t, mcsrModule(gw), "lb")

	var col collector
	require.NoError(t, cmd.Run(context.Background(), mcsrCtx(""), "record", col.emit))
	require.Len(t, col.out, 1)
	assert.Contains(t, col.out[0].Text, "nobody on this leaderboard yet")
}

func TestMcsrLbToggleOff(t *testing.T) {
	gw := &fakeGossip{replies: map[string]any{"mcsr.leaderboard": gossiprpc.McsrLeaderboardReply{}}}
	cmd := findCmd(t, mcsrModule(gw), "lb")

	var col collector
	require.NoError(t, cmd.Run(context.Background(), mcsrCtx(`{"lbEnabled":"off"}`), "", col.emit))
	assert.Empty(t, col.out)
	assert.Empty(t, gw.calls)
}

func TestMcsrLbSeasonToken(t *testing.T) {
	gw := &fakeGossip{replies: map[string]any{
		"mcsr.leaderboard": gossiprpc.McsrLeaderboardReply{Board: "elo", Empty: true},
	}}
	cmd := findCmd(t, mcsrModule(gw), "lb")

	var col collector
	require.NoError(t, cmd.Run(context.Background(), mcsrCtx(""), "season:11", col.emit))
	assert.Equal(t, 11, gw.lastCall(t).req.Season)
}

func TestMcsrRaceDefaultTemplate(t *testing.T) {
	gw := &fakeGossip{replies: map[string]any{
		"mcsr.weekly_race": gossiprpc.McsrWeeklyRaceReply{
			LeaderName: "gharfyy", LeaderTime: "2:27.374",
			Player: "Feinberg", PlayerTime: "2:40.000", PlayerRank: 2, HasPlayer: true,
		},
	}}
	cmd := findCmd(t, mcsrModule(gw), "race")

	var col collector
	require.NoError(t, cmd.Run(context.Background(), mcsrCtx(""), "", col.emit))
	require.Len(t, col.out, 1)
	assert.Equal(t, "#1 gharfyy (2:27.374) · Feinberg: 2:40.000 (#2)", col.out[0].Text)
}

func TestMcsrRaceNoPlayerTime(t *testing.T) {
	gw := &fakeGossip{replies: map[string]any{
		"mcsr.weekly_race": gossiprpc.McsrWeeklyRaceReply{LeaderName: "gharfyy", LeaderTime: "2:27.374", Player: "Newbie", HasPlayer: false},
	}}
	cmd := findCmd(t, mcsrModule(gw), "race")

	var col collector
	require.NoError(t, cmd.Run(context.Background(), mcsrCtx(""), "", col.emit))
	require.Len(t, col.out, 1)
	assert.Contains(t, col.out[0].Text, "#1 gharfyy (2:27.374)")
	assert.Contains(t, col.out[0].Text, "no time in this week's race yet")
}

func TestMcsrRaceEmpty(t *testing.T) {
	gw := &fakeGossip{replies: map[string]any{"mcsr.weekly_race": gossiprpc.McsrWeeklyRaceReply{Empty: true}}}
	cmd := findCmd(t, mcsrModule(gw), "race")

	var col collector
	require.NoError(t, cmd.Run(context.Background(), mcsrCtx(""), "", col.emit))
	require.Len(t, col.out, 1)
	assert.Contains(t, col.out[0].Text, "no times submitted for this week's race yet")
}

func TestMcsrRaceToggleOff(t *testing.T) {
	gw := &fakeGossip{replies: map[string]any{"mcsr.weekly_race": gossiprpc.McsrWeeklyRaceReply{}}}
	cmd := findCmd(t, mcsrModule(gw), "race")

	var col collector
	require.NoError(t, cmd.Run(context.Background(), mcsrCtx(`{"raceEnabled":"off"}`), "", col.emit))
	assert.Empty(t, col.out)
	assert.Empty(t, gw.calls)
}

// --- parseMcsrSeason ---------------------------------------------------------------

func TestParseMcsrSeason(t *testing.T) {
	cases := []struct {
		name       string
		args       string
		wantRest   string
		wantSeason int
	}{
		{"no token", "Feinberg", "Feinberg", 0},
		{"trailing token", "Feinberg season:11", "Feinberg", 11},
		{"leading token", "season:11 Feinberg", "Feinberg", 11},
		{"token only", "season:11", "", 11},
		{"invalid number ignored", "Feinberg season:abc", "Feinberg season:abc", 0},
		{"zero ignored", "Feinberg season:0", "Feinberg season:0", 0},
		{"empty", "", "", 0},
		{"case insensitive prefix", "Feinberg SEASON:9", "Feinberg", 9},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rest, season := parseMcsrSeason(tc.args)
			assert.Equal(t, tc.wantRest, rest)
			assert.Equal(t, tc.wantSeason, season)
		})
	}
}

// !elo must keep behaving exactly as before this feature: no season token
// means no Season on the wire, same request shape as today.
func TestMcsrEloNoSeasonTokenUnchanged(t *testing.T) {
	gw := &fakeGossip{replies: map[string]any{
		"mcsr.user": gossiprpc.McsrUserReply{Nickname: "Feinberg", Elo: 1650, Rank: 12},
	}}
	cmd := findCmd(t, mcsrModule(gw), "elo")

	var col collector
	require.NoError(t, cmd.Run(context.Background(), mcsrCtx(""), "", col.emit))
	assert.Zero(t, gw.lastCall(t).req.Season)
}

func TestMcsrEloSeasonToken(t *testing.T) {
	gw := &fakeGossip{replies: map[string]any{
		"mcsr.user": gossiprpc.McsrUserReply{Nickname: "Feinberg", Elo: 1650, Rank: 12},
	}}
	cmd := findCmd(t, mcsrModule(gw), "elo")

	var col collector
	require.NoError(t, cmd.Run(context.Background(), mcsrCtx(""), "Feinberg season:5", col.emit))
	call := gw.lastCall(t)
	assert.Equal(t, "Feinberg", call.req.Account)
	assert.Equal(t, 5, call.req.Season)
}

// --- !pb ---------------------------------------------------------------------------

func TestMcsrPbBareIsAllTime(t *testing.T) {
	gw := &fakeGossip{replies: map[string]any{
		"paceman.personal_best": gossiprpc.PacemanPersonalBestReply{Player: "Feinberg", Window: "all-time", Time: "6:10.012"},
	}}
	cmd := findCmd(t, mcsrModule(gw), "pb")

	var col collector
	require.NoError(t, cmd.Run(context.Background(), mcsrCtx(`{"account":"Feinberg"}`), "", col.emit))
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
			cmd := findCmd(t, mcsrModule(gw), "pb")

			var col collector
			require.NoError(t, cmd.Run(context.Background(), mcsrCtx(`{"account":"Feinberg"}`), tc.window, col.emit))
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
	cmd := findCmd(t, mcsrModule(gw), "pb")

	var col collector
	require.NoError(t, cmd.Run(context.Background(), mcsrCtx(`{"account":"Feinberg"}`), "lowk3y_", col.emit))
	call := gw.lastCall(t)
	assert.Equal(t, "lowk3y_", call.req.Account)
	assert.Equal(t, "", call.req.TimeWindow)
}

func TestMcsrPbWindowAndName(t *testing.T) {
	gw := &fakeGossip{replies: map[string]any{
		"paceman.personal_best": gossiprpc.PacemanPersonalBestReply{Player: "lowk3y_", Window: "weekly", Time: "6:01.500"},
	}}
	cmd := findCmd(t, mcsrModule(gw), "pb")

	var col collector
	require.NoError(t, cmd.Run(context.Background(), mcsrCtx(`{"account":"Feinberg"}`), "weekly lowk3y_", col.emit))
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
	cmd := findCmd(t, mcsrModule(gw), "pb")

	var col collector
	require.NoError(t, cmd.Run(context.Background(), mcsrCtx(`{"account":"Newbie"}`), "daily", col.emit))
	require.Len(t, col.out, 1)
	assert.Equal(t, "Newbie: no personal best yet (daily)", col.out[0].Text)
}

func TestMcsrPbRanked(t *testing.T) {
	gw := &fakeGossip{replies: map[string]any{
		"mcsr.user": gossiprpc.McsrUserReply{Nickname: "Feinberg", Elo: 1650, BestTimeMS: 595036},
	}}
	cmd := findCmd(t, mcsrModule(gw), "pb")

	var col collector
	require.NoError(t, cmd.Run(context.Background(), mcsrCtx(`{"account":"Feinberg"}`), "ranked", col.emit))
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
	cmd := findCmd(t, mcsrModule(gw), "pb")

	var col collector
	require.NoError(t, cmd.Run(context.Background(), mcsrCtx(`{"account":"Newbie"}`), "ranked", col.emit))
	require.Len(t, col.out, 1)
	assert.Equal(t, "Newbie: no personal best yet (ranked)", col.out[0].Text)
}

func TestMcsrPbToggleOff(t *testing.T) {
	gw := &fakeGossip{replies: map[string]any{
		"paceman.personal_best": gossiprpc.PacemanPersonalBestReply{Player: "Feinberg", Time: "6:10.012"},
	}}
	cmd := findCmd(t, mcsrModule(gw), "pb")

	var col collector
	require.NoError(t, cmd.Run(context.Background(), mcsrCtx(`{"pbEnabled":"off"}`), "", col.emit))
	assert.Empty(t, col.out)
	assert.Empty(t, gw.calls)
}

func TestMcsrPbUpstream4xxChatsBack(t *testing.T) {
	gw := &fakeGossip{err: bus.RPCReplyError{Message: "player not found"}}
	cmd := findCmd(t, mcsrModule(gw), "pb")

	var col collector
	require.NoError(t, cmd.Run(context.Background(), mcsrCtx(""), "ghostplayer", col.emit))
	require.Len(t, col.out, 1)
	assert.Equal(t, "ghostplayer: player not found", col.out[0].Text)
}

// --- parseMcsrPbArgs -----------------------------------------------------------------

func TestParseMcsrPbArgs(t *testing.T) {
	cases := []struct {
		name       string
		args       string
		wantWindow string
		wantRest   string
	}{
		{"bare", "", "", ""},
		{"bare name", "Feinberg", "", "Feinberg"},
		{"window only", "daily", "daily", ""},
		{"window and name", "weekly Feinberg", "weekly", "Feinberg"},
		{"ranked keyword", "ranked", "ranked", ""},
		{"case insensitive window", "DAILY", "daily", ""},
		{"unrecognized first word is a name", "monthlyish", "", "monthlyish"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			window, rest := parseMcsrPbArgs(tc.args)
			assert.Equal(t, tc.wantWindow, window)
			assert.Equal(t, tc.wantRest, rest)
		})
	}
}
