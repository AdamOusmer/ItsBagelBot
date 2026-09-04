// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package modules

import (
	"context"
	"testing"

	"ItsBagelBot/app/twitch/sesame/engine"
	"ItsBagelBot/app/twitch/sesame/module"
	"ItsBagelBot/internal/domain/outgress"
	gossiprpc "ItsBagelBot/internal/domain/rpc/gossip"
	"ItsBagelBot/pkg/bus"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func clashCmd(t *testing.T, gw engine.GossipCaller, name string) module.Command {
	t.Helper()
	m := ClashRoyale(engine.Deps{Gossip: gw, Log: zap.NewNop()})
	assert.Equal(t, "clashroyale", m.Name)
	assert.Equal(t, module.KindOptIn, m.Kind)
	return findCmd(t, m, name)
}

func clashStatsReply() gossiprpc.ClashRoyaleStatsReply {
	return gossiprpc.ClashRoyaleStatsReply{
		Player: "Bagel", Tag: "#P2LQ0GR", KingLevel: 62,
		Wins: 600, Losses: 300, Draws: 100, Battles: 1000, WinRate: 60,
		ThreeCrownWins: 120,
		Clan:           gossiprpc.ClashRoyaleClan{Tag: "#2Q0", Name: "Bakery"},
		FavouriteCard:  gossiprpc.ClashRoyaleCard{Name: "Knight"},
	}
}

func TestCrstatsDefaultTemplate(t *testing.T) {
	gw := &fakeGossip{replies: map[string]any{"clashroyale.stats": clashStatsReply()}}
	cmd := clashCmd(t, gw, "crstats")

	var col collector
	require.NoError(t, cmd.Run(context.Background(), urchinCtx(""), "", col.emit))
	require.Len(t, col.out, 1)
	assert.Equal(t, outgress.TypeChat, col.out[0].Type)
	assert.Equal(t, "2", col.out[0].BroadcasterID)
	assert.Equal(t,
		"Bagel · level 62 · 600W/300L · 60% WR · 120 three-crowns · Bakery",
		col.out[0].Text)

	// No linked tag and no arg: falls back to the broadcaster's login, and the
	// premium flag rides along for gossip's reserved bucket.
	call := gw.lastCall(t)
	assert.Equal(t, "streamer", call.req.Account)
	assert.False(t, call.req.IsPremium)
}

func TestCrstatsLinkedTagAndArgPriority(t *testing.T) {
	gw := &fakeGossip{replies: map[string]any{"clashroyale.stats": clashStatsReply()}}
	cmd := clashCmd(t, gw, "crstats")

	var col collector
	cfg := `{"account":"#P2LQ0GR"}`
	require.NoError(t, cmd.Run(context.Background(), urchinCtx(cfg), "", col.emit))
	assert.Equal(t, "#P2LQ0GR", gw.lastCall(t).req.Account)

	// An explicit argument beats the linked tag; '@' and trailing words strip.
	require.NoError(t, cmd.Run(context.Background(), urchinCtx(cfg), "@#P9VQ0JR please", col.emit))
	assert.Equal(t, "#P9VQ0JR", gw.lastCall(t).req.Account)
}

func TestCrDecksRankedRoadTemplates(t *testing.T) {
	decks := gossiprpc.ClashRoyaleDecksReply{
		Player: "Bagel", Tag: "#P2LQ0GR",
		CurrentDeck: []gossiprpc.ClashRoyaleCard{
			{Name: "Knight"}, {Name: "Archers"}, {Name: "Fireball"},
		},
		SupportCards:  []gossiprpc.ClashRoyaleCard{{Name: "Tower Troop"}},
		AverageElixir: 3.75,
	}
	ranked := gossiprpc.ClashRoyaleRankedReply{
		Player: "Bagel", Tag: "#P2LQ0GR",
		Current: gossiprpc.ClashRoyaleRankedResult{LeagueNumber: 10, Trophies: 2100, Rank: 321},
		Best:    gossiprpc.ClashRoyaleRankedResult{LeagueNumber: 10, Trophies: 2400, Rank: 42},
	}
	road := gossiprpc.ClashRoyaleTrophyRoadReply{
		Player: "Bagel", Tag: "#P2LQ0GR", Trophies: 9123, BestTrophies: 9345,
		Arena: gossiprpc.ClashRoyaleArena{Name: "Legendary Arena"},
	}

	cases := []struct {
		name, trigger, endpoint, want string
		reply                         any
	}{
		{"decks", "crdecks", "decks",
			"Bagel's deck (3/8): Knight, Archers, Fireball · avg elixir 3.75", decks},
		{"ranked", "crranked", "ranked",
			"Bagel Path of Legends: league 10 · 2100 trophies · rank #321 · best 2400", ranked},
		{"road", "crroad", "trophy_road",
			"Bagel: 9123 trophies · best 9345 · Legendary Arena", road},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gw := &fakeGossip{replies: map[string]any{"clashroyale." + tc.endpoint: tc.reply}}
			var col collector
			require.NoError(t, clashCmd(t, gw, tc.trigger).Run(context.Background(), urchinCtx(""), "", col.emit))
			require.Len(t, col.out, 1)
			assert.Equal(t, tc.want, col.out[0].Text)
			assert.Equal(t, tc.endpoint, gw.lastCall(t).endpoint)
		})
	}
}

// The !cr root routes its first argument word: bare/tag → profile stats,
// decks/ranked/road select the subcommand, and the remainder is the tag arg.
func TestCrDispatch(t *testing.T) {
	cases := []struct {
		name, args, endpoint string
	}{
		{"bare is stats", "", "stats"},
		{"tag arg is stats", "@#P2LQ0GR extra", "stats"},
		{"decks subcommand", "decks", "decks"},
		{"deck alias with tag", "deck #P2LQ0GR", "decks"},
		{"ranked subcommand", "ranked", "ranked"},
		{"pol alias", "POL", "ranked"},
		{"road subcommand", "road", "trophy_road"},
		{"trophy alias", "trophies", "trophy_road"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gw := &fakeGossip{replies: map[string]any{
				"clashroyale.stats":       clashStatsReply(),
				"clashroyale.decks":       gossiprpc.ClashRoyaleDecksReply{Player: "Bagel"},
				"clashroyale.ranked":      gossiprpc.ClashRoyaleRankedReply{Player: "Bagel"},
				"clashroyale.trophy_road": gossiprpc.ClashRoyaleTrophyRoadReply{Player: "Bagel"},
			}}
			var col collector
			require.NoError(t, clashCmd(t, gw, "cr").Run(context.Background(), urchinCtx(`{"account":"#P2LQ0GR"}`), tc.args, col.emit))
			require.Len(t, col.out, 1)
			assert.Equal(t, tc.endpoint, gw.lastCall(t).endpoint)
			// Every case targets the same player: arg-bearing cases type the
			// linked tag itself and bare subcommands fall back to it.
			assert.Equal(t, "#P2LQ0GR", gw.lastCall(t).req.Account)
		})
	}
}

// A per-command "off" toggle keeps that command silent: no chat line and no
// gossip call.
func TestClashDisabledStaysSilent(t *testing.T) {
	cases := []struct{ name, config string }{
		{"cr", `{"statsEnabled":"off"}`},
		{"crstats", `{"statsEnabled":"off"}`},
		{"crdecks", `{"decksEnabled":"off"}`},
		{"crranked", `{"rankedEnabled":"off"}`},
		{"crroad", `{"roadEnabled":"off"}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gw := &fakeGossip{}
			var col collector
			require.NoError(t, clashCmd(t, gw, tc.name).Run(context.Background(), urchinCtx(tc.config), "", col.emit))
			assert.Empty(t, col.out)
			gw.mu.Lock()
			assert.Empty(t, gw.calls)
			gw.mu.Unlock()
		})
	}
}

func TestCrReplyErrorChats(t *testing.T) {
	gw := &fakeGossip{err: bus.RPCReplyError{Message: "player not found"}}
	var col collector
	require.NoError(t, clashCmd(t, gw, "crstats").Run(context.Background(), urchinCtx(""), "#P0AAAAAA", col.emit))
	require.Len(t, col.out, 1)
	assert.Equal(t, "#P0AAAAAA: player not found", col.out[0].Text)
}

// An unranked player replaces the ranked template entirely: every numeric
// token would render zero, so the default line says why instead.
func TestCrrankedUnranked(t *testing.T) {
	gw := &fakeGossip{replies: map[string]any{"clashroyale.ranked": gossiprpc.ClashRoyaleRankedReply{
		Player: "Bagel", Unranked: true,
	}}}
	var col collector
	require.NoError(t, clashCmd(t, gw, "crranked").Run(context.Background(), urchinCtx(""), "", col.emit))
	require.Len(t, col.out, 1)
	assert.Equal(t, "Bagel has no Path of Legends record this season", col.out[0].Text)
}
