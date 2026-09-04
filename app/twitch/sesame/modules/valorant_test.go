// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package modules

import (
	"context"
	"testing"
	"time"

	"ItsBagelBot/app/twitch/sesame/engine"
	"ItsBagelBot/app/twitch/sesame/module"
	"ItsBagelBot/internal/domain/outgress"
	gossiprpc "ItsBagelBot/internal/domain/rpc/gossip"
	"ItsBagelBot/pkg/bus"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func valCmd(t *testing.T, gw engine.GossipCaller, name string) module.Command {
	t.Helper()
	m := Valorant(engine.Deps{Gossip: gw, Log: zap.NewNop()})
	assert.Equal(t, "valorant", m.Name)
	assert.Equal(t, module.KindOptIn, m.Kind)
	return findCmd(t, m, name)
}

func valRankReply() gossiprpc.ValorantRankReply {
	return gossiprpc.ValorantRankReply{
		Player: "Frosty#EUW1", Region: "eu",
		Tier: "Immortal 2", Elo: 1832, RR: 67, LastChange: -12,
		PeakTier: "Immortal 1", Placement: 513,
	}
}

func TestValrankDefaultTemplate(t *testing.T) {
	gw := &fakeGossip{replies: map[string]any{"valorant.rank": valRankReply()}}
	cmd := valCmd(t, gw, "valrank")

	var col collector
	require.NoError(t, cmd.Run(context.Background(), urchinCtx(""), "", col.emit))
	require.Len(t, col.out, 1)
	assert.Equal(t, outgress.TypeChat, col.out[0].Type)
	assert.Equal(t, "2", col.out[0].BroadcasterID)
	assert.Equal(t,
		"Frosty#EUW1 · Immortal 2 · 67 RR (-12) · peak Immortal 1",
		col.out[0].Text)

	// No linked id and no arg: falls back to the broadcaster's login (which
	// the provider rejects as an invalid Riot ID), and the premium flag rides
	// along for gossip's reserved bucket.
	call := gw.lastCall(t)
	assert.Equal(t, "streamer", call.req.Account)
	assert.False(t, call.req.IsPremium)
}

// Scoping inputs resolve in priority order: chat arg over dashboard config,
// shard/ladder words wherever they sit around the id.
func TestValScopingArgsAndConfig(t *testing.T) {
	gw := &fakeGossip{replies: map[string]any{"valorant.rank": valRankReply()}}
	cmd := valCmd(t, gw, "valrank")
	var col collector

	// Config alone scopes both axes.
	cfg := `{"account":"Frosty#EUW1","region":"eu","platform":"pc"}`
	require.NoError(t, cmd.Run(context.Background(), urchinCtx(cfg), "", col.emit))
	call := gw.lastCall(t)
	assert.Equal(t, "Frosty#EUW1", call.req.Account)
	assert.Equal(t, "eu", call.req.Region)
	assert.Equal(t, "pc", call.req.Platform)

	// An explicit id beats the linked one; a shard word is peeled wherever it
	// sits and overrides the configured region.
	require.NoError(t, cmd.Run(context.Background(), urchinCtx(cfg), "console @Reyna#KR5 ap", col.emit))
	call = gw.lastCall(t)
	assert.Equal(t, "Reyna#KR5", call.req.Account)
	assert.Equal(t, "ap", call.req.Region)
	assert.Equal(t, "console", call.req.Platform)
}

// The !val root routes its first argument word onto the subcommand runners;
// anything else targets rank, so "!val Frosty#EUW1" reads naturally.
func TestValDispatch(t *testing.T) {
	replies := map[string]any{
		"valorant.rank":        gossiprpc.ValorantRankReply{Player: "Frosty#EUW1"},
		"valorant.matches":     gossiprpc.ValorantMatchesReply{Player: "Frosty#EUW1"},
		"valorant.account":     gossiprpc.ValorantAccountReply{Player: "Frosty#EUW1", AccountLevel: 142},
		"valorant.leaderboard": gossiprpc.ValorantLeaderboardReply{Board: "ap/console"},
		"valorant.shop":        gossiprpc.ValorantShopReply{},
	}
	cases := []struct {
		name, args, endpoint string
	}{
		{"bare is rank", "", "rank"},
		{"id arg is rank", "Frosty#EUW1", "rank"},
		{"standing alias", "standing", "rank"},
		{"matches subcommand", "matches", "matches"},
		{"history alias with id", "history Frosty#EUW1", "matches"},
		{"account subcommand", "account", "account"},
		{"who alias", "WHO", "account"},
		{"lb subcommand with shard", "lb console ap", "leaderboard"},
		{"leaderboard alias", "leaderboard", "leaderboard"},
		{"shop subcommand", "shop", "shop"},
		{"rotation alias", "rotation", "shop"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gw := &fakeGossip{replies: replies}
			var col collector
			require.NoError(t, valCmd(t, gw, "val").Run(context.Background(),
				urchinCtx(`{"account":"Frosty#EUW1","region":"eu"}`), tc.args, col.emit))
			require.Len(t, col.out, 1)
			assert.Equal(t, tc.endpoint, gw.lastCall(t).endpoint)
		})
	}
}

// A per-command "off" toggle keeps that command silent: no chat line and no
// gossip call.
func TestValDisabledStaysSilent(t *testing.T) {
	cases := []struct{ name, config string }{
		{"val", `{"rankEnabled":"off"}`},
		{"valrank", `{"rankEnabled":"off"}`},
		{"valmatches", `{"matchesEnabled":"off"}`},
		{"valaccount", `{"accountEnabled":"off"}`},
		{"vallb", `{"boardEnabled":"off"}`},
		{"valshop", `{"shopEnabled":"off"}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gw := &fakeGossip{}
			var col collector
			require.NoError(t, valCmd(t, gw, tc.name).Run(context.Background(), urchinCtx(tc.config), "", col.emit))
			assert.Empty(t, col.out)
			gw.mu.Lock()
			assert.Empty(t, gw.calls)
			gw.mu.Unlock()
		})
	}
}

func TestValReplyErrorChats(t *testing.T) {
	gw := &fakeGossip{err: bus.RPCReplyError{Message: "player not found"}}
	var col collector
	require.NoError(t, valCmd(t, gw, "valrank").Run(context.Background(), urchinCtx(""), "Frosty#EUW1", col.emit))
	require.Len(t, col.out, 1)
	assert.Equal(t, "Frosty#EUW1: player not found", col.out[0].Text)

	// The accountless shop names the feature instead of a target player.
	var shop collector
	require.NoError(t, valCmd(t, gw, "valshop").Run(context.Background(), urchinCtx(""), "", shop.emit))
	require.Len(t, shop.out, 1)
	assert.Equal(t, "daily rotation: player not found", shop.out[0].Text)
}

// An unranked player replaces the rank template entirely: every numeric token
// would render zero, so the default line says why instead.
func TestValrankUnranked(t *testing.T) {
	gw := &fakeGossip{replies: map[string]any{"valorant.rank": gossiprpc.ValorantRankReply{
		Player: "Frosty#EUW1", Unranked: true,
	}}}
	var col collector
	require.NoError(t, valCmd(t, gw, "valrank").Run(context.Background(), urchinCtx(""), "", col.emit))
	require.Len(t, col.out, 1)
	assert.Equal(t, "Frosty#EUW1 has no competitive record this act", col.out[0].Text)
}

func TestValmatchesTemplateAndEmpty(t *testing.T) {
	full := gossiprpc.ValorantMatchesReply{
		Player: "Frosty#EUW1", Region: "eu",
		Matches: []gossiprpc.ValorantMatchEntry{
			{Map: "Haven", Agent: "Jett", Result: "win", Kills: 20, Deaths: 14, Assists: 7, ACS: 231.4, AgoSeconds: 7200},
			{Map: "Ascent", Agent: "Omen", Result: "loss", Kills: 9, Deaths: 17, Assists: 3, AgoSeconds: 60},
		},
	}
	t.Run("default template", func(t *testing.T) {
		gw := &fakeGossip{replies: map[string]any{"valorant.matches": full}}
		var col collector
		require.NoError(t, valCmd(t, gw, "valmatches").Run(context.Background(), urchinCtx(""), "", col.emit))
		require.Len(t, col.out, 1)
		assert.Equal(t,
			"Frosty#EUW1's last 2: Jett 20/14/7 win on Haven, Omen 9/17/3 loss on Ascent",
			col.out[0].Text)
	})
	t.Run("empty replaces the template", func(t *testing.T) {
		gw := &fakeGossip{replies: map[string]any{"valorant.matches": gossiprpc.ValorantMatchesReply{
			Player: "Frosty#EUW1", Empty: true,
		}}}
		var col collector
		require.NoError(t, valCmd(t, gw, "valmatches").Run(context.Background(), urchinCtx(""), "", col.emit))
		require.Len(t, col.out, 1)
		assert.Equal(t, "Frosty#EUW1 has no recent competitive games", col.out[0].Text)
	})
}

// A bare !vallb is a regional top-N ask: the board answers even though neither
// an arg nor a linked id exists, because the Twitch-login fallback would only
// mint an invalid-Riot-ID error.
func TestVallbBareTopN(t *testing.T) {
	gw := &fakeGossip{replies: map[string]any{"valorant.leaderboard": gossiprpc.ValorantLeaderboardReply{
		Board: "ap/console",
		Entries: []gossiprpc.ValorantLeaderboardEntry{
			{Rank: 4, Player: "Zekken#5221", Tier: 25, RR: 431, Wins: 61},
			{Rank: 5, Player: "Frosty#EUW1", Tier: 25, RR: 402},
		},
	}}}
	var col collector
	require.NoError(t, valCmd(t, gw, "vallb").Run(context.Background(),
		urchinCtx(`{"region":"ap","platform":"console"}`), "", col.emit))
	require.Len(t, col.out, 1)
	assert.Equal(t,
		"ap/console: #4 Zekken#5221 (431 RR), #5 Frosty#EUW1 (402 RR)",
		col.out[0].Text)

	call := gw.lastCall(t)
	assert.Empty(t, call.req.Account)
	assert.Equal(t, "ap", call.req.Region)
	assert.Equal(t, "console", call.req.Platform)
}

// An explicit id still scopes a leaderboard ask ("is anyone I know on the
// board?"), riding through as the request's account.
func TestVallbWithID(t *testing.T) {
	gw := &fakeGossip{replies: map[string]any{"valorant.leaderboard": gossiprpc.ValorantLeaderboardReply{
		Player: "Frosty#EUW1", Board: "eu/pc",
		Entries: []gossiprpc.ValorantLeaderboardEntry{{Rank: 513, Player: "Frosty#EUW1", Tier: 24, RR: 1832}},
	}}}
	var col collector
	require.NoError(t, valCmd(t, gw, "vallb").Run(context.Background(),
		urchinCtx(""), "eu Frosty#EUW1", col.emit))
	require.Len(t, col.out, 1)
	assert.Equal(t, "eu/pc: #513 Frosty#EUW1 (1832 RR)", col.out[0].Text)
	assert.Equal(t, "Frosty#EUW1", gw.lastCall(t).req.Account)
}

func TestValaccountTemplate(t *testing.T) {
	gw := &fakeGossip{replies: map[string]any{"valorant.account": gossiprpc.ValorantAccountReply{
		Player: "Frosty#EUW1", Region: "eu", AccountLevel: 142, Title: "Radiant",
	}}}
	var col collector
	require.NoError(t, valCmd(t, gw, "valaccount").Run(context.Background(), urchinCtx(""), "", col.emit))
	require.Len(t, col.out, 1)
	assert.Equal(t, "Frosty#EUW1 · account level 142", col.out[0].Text)
}

func TestValshopTemplate(t *testing.T) {
	gw := &fakeGossip{replies: map[string]any{"valorant.shop": gossiprpc.ValorantShopReply{
		ResetUnix: time.Now().Add(2*time.Hour + 30*time.Minute).Unix(),
		Items: []gossiprpc.ValorantShopItem{
			{Name: "Reaver Vandal", Price: 1775, Tier: "Exclusive"},
			{Name: "Ion Frenzy", Price: 875},
		},
		Count: 2,
	}}}
	var col collector
	require.NoError(t, valCmd(t, gw, "valshop").Run(context.Background(), urchinCtx(""), "", col.emit))
	require.Len(t, col.out, 1)
	assert.Equal(t,
		"Daily rotation (2): Reaver Vandal (1775 VP), Ion Frenzy (875 VP) · resets in 2h 30m",
		col.out[0].Text)
}
