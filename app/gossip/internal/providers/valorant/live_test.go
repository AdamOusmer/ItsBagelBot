// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package valorant

import (
	"fmt"
	"os"
	"strings"
	"testing"

	"ItsBagelBot/app/gossip/internal/providertest"
	gossiprpc "ItsBagelBot/internal/domain/rpc/gossip"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func liveProbeEnabled() bool {
	return os.Getenv("VALORANT_LIVE") == "1" && os.Getenv("VALORANT_API_KEY") != ""
}

func TestLiveUpstream(t *testing.T) {
	name, tag, linked := strings.Cut(os.Getenv("VALORANT_LIVE_RIOT_ID"), "#")
	if !liveProbeEnabled() || !linked {
		t.Skip("live probe: needs VALORANT_LIVE=1, a real VALORANT_API_KEY and your own VALORANT_LIVE_RIOT_ID (Name#Tag)")
	}
	d := providertest.Deps(providertest.NewMemStore())
	d.RiotLinks = fakeLinks{linkedChannel: {GameName: name, TagLine: tag, Shard: os.Getenv("VALORANT_LIVE_SHARD")}}
	p := New(Config{APIKey: os.Getenv("VALORANT_API_KEY")}, d)

	board := providertest.Call[leaderboardReply](t, p, "leaderboard", gossiprpc.Request{Region: "eu", Platform: "pc"})
	require.Empty(t, board.Error, "leaderboard probe failed")
	require.NotEmpty(t, board.Entries, "eu/pc board empty")
	top := board.Entries[0]
	fmt.Printf("leaderboard: %s top=%s tier=%d rr=%d\n", board.Board, top.Player, top.Tier, top.RR)
	require.Contains(t, top.Player, "#", "board row missing tagline")

	player := gossiprpc.Request{ChannelID: linkedChannel}

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
}
