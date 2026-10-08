// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package modules

import (
	"testing"
	"time"

	gossiprpc "ItsBagelBot/internal/domain/rpc/gossip"
	"ItsBagelBot/pkg/bus"
)

func valRankReply() gossiprpc.ValorantRankReply {
	return gossiprpc.ValorantRankReply{
		Player: "Frosty#EUW1", Region: "eu",
		Tier: "Immortal 2", Elo: 1832, RR: 67, LastChange: -12,
		PeakTier: "Immortal 1", Placement: 513,
	}
}

func valorantReplies() map[string]any {
	return map[string]any{
		"valorant.rank":        gossiprpc.ValorantRankReply{Player: "Frosty#EUW1"},
		"valorant.matches":     gossiprpc.ValorantMatchesReply{Player: "Frosty#EUW1"},
		"valorant.account":     gossiprpc.ValorantAccountReply{Player: "Frosty#EUW1", AccountLevel: 142},
		"valorant.leaderboard": gossiprpc.ValorantLeaderboardReply{Board: "ap/console"},
		"valorant.shop":        gossiprpc.ValorantShopReply{},
	}
}

func valChat(text, config string) gossipChat {
	return gossipChat{module: "valorant", text: text, config: config}
}

func TestValorantReplies(t *testing.T) {
	rank := map[string]any{"valorant.rank": valRankReply()}
	notFound := bus.RPCReplyError{Message: "player not found"}
	matches := gossiprpc.ValorantMatchesReply{
		Player: "Frosty#EUW1", Region: "eu",
		Matches: []gossiprpc.ValorantMatchEntry{
			{Map: "Haven", Agent: "Jett", Result: "win", Kills: 20, Deaths: 14, Assists: 7, ACS: 231.4, AgoSeconds: 7200},
			{Map: "Ascent", Agent: "Omen", Result: "loss", Kills: 9, Deaths: 17, Assists: 3, AgoSeconds: 60},
		},
	}
	shop := gossiprpc.ValorantShopReply{
		ResetUnix: time.Now().Add(2*time.Hour + 30*time.Minute).Unix(),
		Items: []gossiprpc.ValorantShopItem{
			{Name: "Reaver Vandal", Price: 1775, Tier: "Exclusive"},
			{Name: "Ion Frenzy", Price: 875},
		},
		Count: 2,
	}
	runGossipCases(t, []gossipCase{
		{name: "valrank default template", chat: valChat("!valrank", ""), replies: rank,
			exact: "Frosty#EUW1 · Immortal 2 · 67 RR (-12) · peak Immortal 1",
			call:  &gossipCall{"valorant.rank", gossiprpc.Request{Account: "streamer"}}},
		{name: "valrank unranked", chat: valChat("!valrank", ""),
			replies: map[string]any{"valorant.rank": gossiprpc.ValorantRankReply{Player: "Frosty#EUW1", Unranked: true}},
			exact:   "Frosty#EUW1 has no competitive record this act"},
		{name: "valrank renders the namespaced saved template", chat: valChat("!valrank", `{"rankMessage":"{valorant:player}: {valorant:tier} ({valorant:rr} RR) {valorant:unknown}"}`),
			replies: rank, exact: "Frosty#EUW1: Immortal 2 (67 RR) {valorant:unknown}"},
		{name: "valmatches default template", chat: valChat("!valmatches", ""), replies: map[string]any{"valorant.matches": matches},
			exact: "Frosty#EUW1's last 2: Jett 20/14/7 win on Haven, Omen 9/17/3 loss on Ascent"},
		{name: "valmatches empty replaces the template", chat: valChat("!valmatches", ""),
			replies: map[string]any{"valorant.matches": gossiprpc.ValorantMatchesReply{Player: "Frosty#EUW1", Empty: true}},
			exact:   "Frosty#EUW1 has no recent competitive games"},
		{name: "vallb bare lists the top of the board", chat: valChat("!vallb", `{"region":"ap","platform":"console"}`),
			replies: map[string]any{"valorant.leaderboard": gossiprpc.ValorantLeaderboardReply{
				Board: "ap/console",
				Entries: []gossiprpc.ValorantLeaderboardEntry{
					{Rank: 4, Player: "Zekken#5221", Tier: 25, RR: 431, Wins: 61},
					{Rank: 5, Player: "Frosty#EUW1", Tier: 25, RR: 402},
				},
			}},
			exact: "ap/console: #4 Zekken#5221 (431 RR), #5 Frosty#EUW1 (402 RR)",
			call:  &gossipCall{"valorant.leaderboard", gossiprpc.Request{Region: "ap", Platform: "console"}}},
		{name: "vallb with an id finds that player", chat: valChat("!vallb eu Frosty#EUW1", ""),
			replies: map[string]any{"valorant.leaderboard": gossiprpc.ValorantLeaderboardReply{
				Player: "Frosty#EUW1", Board: "eu/pc",
				Entries: []gossiprpc.ValorantLeaderboardEntry{{Rank: 513, Player: "Frosty#EUW1", Tier: 24, RR: 1832}},
			}},
			exact: "eu/pc: #513 Frosty#EUW1 (1832 RR)",
			call:  &gossipCall{"valorant.leaderboard", gossiprpc.Request{Account: "Frosty#EUW1", Region: "eu"}}},
		{name: "valaccount default template", chat: valChat("!valaccount", ""),
			replies: map[string]any{"valorant.account": gossiprpc.ValorantAccountReply{Player: "Frosty#EUW1", Region: "eu", AccountLevel: 142, Title: "Radiant"}},
			exact:   "Frosty#EUW1 · account level 142"},
		{name: "valshop default template", chat: valChat("!valshop", ""), replies: map[string]any{"valorant.shop": shop},
			exact: "Daily rotation (2): Reaver Vandal (1775 VP), Ion Frenzy (875 VP) · resets in 2h 30m"},
		{name: "a reply error chats the typed player", chat: valChat("!valrank Frosty#EUW1", ""), err: notFound, exact: "Frosty#EUW1: player not found"},
		{name: "a shop reply error names the rotation", chat: valChat("!valshop", ""), err: notFound, exact: "daily rotation: player not found"},
	})
}

func TestValorantAccountScoping(t *testing.T) {
	const config = `{"account":"Frosty#EUW1","region":"eu","platform":"pc"}`
	const linkedOnly = `{"account":"Frosty#EUW1","region":"eu","platform":"pc","linkedOnly":"on"}`
	rank := map[string]any{"valorant.rank": valRankReply()}
	runGossipCases(t, []gossipCase{
		{name: "config supplies account, region and platform", chat: valChat("!valrank", config), replies: rank, contains: []string{"Immortal 2"},
			call: &gossipCall{"valorant.rank", gossiprpc.Request{Account: "Frosty#EUW1", Region: "eu", Platform: "pc"}}},
		{name: "typed platform, player and region win", chat: valChat("!valrank console @Reyna#KR5 ap", config), replies: rank, contains: []string{"Immortal 2"},
			call: &gossipCall{"valorant.rank", gossiprpc.Request{Account: "Reyna#KR5", Region: "ap", Platform: "console"}}},
		{name: "linkedOnly keeps the linked player but takes the typed shard", chat: valChat("!valrank console @Reyna#KR5 ap", linkedOnly),
			replies: rank, contains: []string{"Immortal 2"},
			call: &gossipCall{"valorant.rank", gossiprpc.Request{Account: "Frosty#EUW1", Region: "ap", Platform: "console"}}},
	})
}

func TestValorantDispatch(t *testing.T) {
	const config = `{"account":"Frosty#EUW1","region":"eu"}`
	routes := []struct{ name, text, route string }{
		{"a bare val is rank", "!val", "valorant.rank"},
		{"an id argument is rank", "!val Frosty#EUW1", "valorant.rank"},
		{"the standing alias is rank", "!val standing", "valorant.rank"},
		{"matches subcommand", "!val matches", "valorant.matches"},
		{"the history alias with an id", "!val history Frosty#EUW1", "valorant.matches"},
		{"account subcommand", "!val account", "valorant.account"},
		{"the who alias is case insensitive", "!val WHO", "valorant.account"},
		{"lb subcommand with a shard", "!val lb console ap", "valorant.leaderboard"},
		{"the leaderboard alias", "!val leaderboard", "valorant.leaderboard"},
		{"shop subcommand", "!val shop", "valorant.shop"},
		{"the rotation alias", "!val rotation", "valorant.shop"},
	}
	var cases []gossipCase
	for _, r := range routes {
		cases = append(cases, gossipCase{name: r.name, chat: valChat(r.text, config), replies: valorantReplies(), route: r.route})
	}
	runGossipCases(t, cases)
}

func TestValorantDisabledCommandsStaySilent(t *testing.T) {
	var cases []gossipCase
	for _, tc := range []struct{ trigger, config string }{
		{"val", `{"rankEnabled":"off"}`},
		{"valrank", `{"rankEnabled":"off"}`},
		{"valmatches", `{"matchesEnabled":"off"}`},
		{"valaccount", `{"accountEnabled":"off"}`},
		{"vallb", `{"boardEnabled":"off"}`},
		{"valshop", `{"shopEnabled":"off"}`},
	} {
		cases = append(cases, gossipCase{name: tc.trigger, chat: valChat("!"+tc.trigger, tc.config), silent: true})
	}
	runGossipCases(t, cases)
}
