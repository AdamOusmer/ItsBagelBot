// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package valorant

import (
	"fmt"
	"os"
	"testing"

	"ItsBagelBot/app/gossip/internal/providertest"
	gossiprpc "ItsBagelBot/internal/domain/rpc/gossip"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLiveUpstream(t *testing.T) {
	if os.Getenv("VALORANT_LIVE") != "1" || os.Getenv("VALORANT_API_KEY") == "" {
		t.Skip("live probe: needs VALORANT_LIVE=1 and a real VALORANT_API_KEY")
	}
	p := New(Config{APIKey: os.Getenv("VALORANT_API_KEY")}, providertest.Deps(providertest.NewMemStore()))

	board := providertest.Call[leaderboardReply](t, p, "leaderboard", gossiprpc.Request{Region: "eu", Platform: "pc"})
	require.Empty(t, board.Error, "leaderboard probe failed")
	require.NotEmpty(t, board.Entries, "eu/pc board empty")
	top := board.Entries[0]
	fmt.Printf("leaderboard: %s top=%s tier=%d rr=%d\n", board.Board, top.Player, top.Tier, top.RR)
	require.Contains(t, top.Player, "#", "board row missing tagline")

	player := gossiprpc.Request{Account: top.Player}

	account := providertest.Call[accountReply](t, p, "account", player)
	assert.Empty(t, account.Error)
	fmt.Printf("account: %s region=%s level=%d card=%v\n", account.Player, account.Region, account.AccountLevel, account.Card != "")

	rank := providertest.Call[rankReply](t, p, "rank", player)
	assert.Empty(t, rank.Error)
	fmt.Printf("rank: %s tier=%q rr=%d delta=%d peak=%q unranked=%v\n",
		rank.Region, rank.Tier, rank.RR, rank.LastChange, rank.PeakTier, rank.Unranked)

	matches := providertest.Call[matchesReply](t, p, "matches", player)
	assert.Empty(t, matches.Error)
	for i, m := range matches.Matches {
		fmt.Printf("match[%d]: %s on %s as %s %d/%d/%d acs=%.1f\n",
			i, m.Result, m.Map, m.Agent, m.Kills, m.Deaths, m.Assists, m.ACS)
	}

	shop := providertest.Call[shopReply](t, p, "shop", gossiprpc.Request{})
	assert.Empty(t, shop.Error)
	if shop.Count > 0 {
		fmt.Printf("shop: bundle=%q (%.1f%% off) price=%d vp items=%d first=%q expires_in=%.1fd\n",
			shop.Bundle, shop.DiscountPct, shop.Price, shop.Count, shop.Items[0].Name,
			float64(shop.ExpiresSeconds)/86400)
	} else {
		fmt.Println("shop: no featured bundle payload")
	}
}
