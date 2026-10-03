// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package modules

import (
	"testing"

	gossiprpc "ItsBagelBot/internal/domain/rpc/gossip"
	"ItsBagelBot/pkg/bus"
)

func clashStatsReply() gossiprpc.ClashRoyaleStatsReply {
	return gossiprpc.ClashRoyaleStatsReply{
		Player: "Bagel", Tag: "#P2LQ0GR", KingLevel: 62,
		Wins: 600, Losses: 300, Draws: 100, Battles: 1000, WinRate: 60,
		ThreeCrownWins: 120,
		Clan:           gossiprpc.ClashRoyaleClan{Tag: "#2Q0", Name: "Bakery"},
		FavouriteCard:  gossiprpc.ClashRoyaleCard{Name: "Knight"},
	}
}

func clashChat(text, config string) gossipChat {
	return gossipChat{module: "clashroyale", text: text, config: config}
}

func TestClashRoyaleReplies(t *testing.T) {
	stats := map[string]any{"clashroyale.stats": clashStatsReply()}
	decks := gossiprpc.ClashRoyaleDecksReply{
		Player: "Bagel", Tag: "#P2LQ0GR",
		CurrentDeck:   []gossiprpc.ClashRoyaleCard{{Name: "Knight"}, {Name: "Archers"}, {Name: "Fireball"}},
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
	const linked = `{"account":"#P2LQ0GR"}`
	runGossipCases(t, []gossipCase{
		{name: "crstats default template", chat: clashChat("!crstats", ""), replies: stats,
			exact: "Bagel · level 62 · 600W/300L · 60% WR · 120 three-crowns · Bakery",
			call:  &gossipCall{"clashroyale.stats", gossiprpc.Request{Account: "streamer"}}},
		{name: "crstats renders the namespaced saved template", chat: clashChat("!crstats", `{"statsMessage":"{clashroyale:player}: {clashroyale:wins}W/{clashroyale:losses}L in {clashroyale:clan}"}`),
			replies: stats, exact: "Bagel: 600W/300L in Bakery"},
		{name: "a linked tag is used without an argument", chat: clashChat("!crstats", linked), replies: stats,
			call: &gossipCall{"clashroyale.stats", gossiprpc.Request{Account: "#P2LQ0GR"}}, contains: []string{"Bagel"}},
		{name: "a typed tag beats the linked one", chat: clashChat("!crstats @#P9VQ0JR please", linked), replies: stats,
			call: &gossipCall{"clashroyale.stats", gossiprpc.Request{Account: "#P9VQ0JR"}}, contains: []string{"Bagel"}},
		{name: "crdecks default template", chat: clashChat("!crdecks", ""), replies: map[string]any{"clashroyale.decks": decks},
			exact: "Bagel's deck (3/8): Knight, Archers, Fireball · avg elixir 3.75", route: "clashroyale.decks"},
		{name: "crranked default template", chat: clashChat("!crranked", ""), replies: map[string]any{"clashroyale.ranked": ranked},
			exact: "Bagel Path of Legends: league 10 · 2100 trophies · rank #321 · best 2400", route: "clashroyale.ranked"},
		{name: "crroad default template", chat: clashChat("!crroad", ""), replies: map[string]any{"clashroyale.trophy_road": road},
			exact: "Bagel: 9123 trophies · best 9345 · Legendary Arena", route: "clashroyale.trophy_road"},
		{name: "crranked unranked", chat: clashChat("!crranked", ""),
			replies: map[string]any{"clashroyale.ranked": gossiprpc.ClashRoyaleRankedReply{Player: "Bagel", Unranked: true}},
			exact:   "Bagel has no Path of Legends record this season"},
		{name: "a reply error chats the typed tag", chat: clashChat("!crstats #P0AAAAAA", ""), err: bus.RPCReplyError{Message: "player not found"},
			exact: "#P0AAAAAA: player not found"},
	})
}

func TestClashRoyaleDispatch(t *testing.T) {
	replies := map[string]any{
		"clashroyale.stats":       clashStatsReply(),
		"clashroyale.decks":       gossiprpc.ClashRoyaleDecksReply{Player: "Bagel"},
		"clashroyale.ranked":      gossiprpc.ClashRoyaleRankedReply{Player: "Bagel"},
		"clashroyale.trophy_road": gossiprpc.ClashRoyaleTrophyRoadReply{Player: "Bagel"},
	}
	routes := []struct{ name, text, endpoint string }{
		{"a bare cr is stats", "!cr", "stats"},
		{"a tag argument is stats", "!cr @#P2LQ0GR extra", "stats"},
		{"decks subcommand", "!cr decks", "decks"},
		{"the deck alias with a tag", "!cr deck #P2LQ0GR", "decks"},
		{"ranked subcommand", "!cr ranked", "ranked"},
		{"the pol alias is case insensitive", "!cr POL", "ranked"},
		{"road subcommand", "!cr road", "trophy_road"},
		{"the trophies alias", "!cr trophies", "trophy_road"},
	}
	var cases []gossipCase
	for _, r := range routes {
		cases = append(cases, gossipCase{name: r.name, chat: clashChat(r.text, `{"account":"#P2LQ0GR"}`), replies: replies,
			call: &gossipCall{"clashroyale." + r.endpoint, gossiprpc.Request{Account: "#P2LQ0GR"}}})
	}
	runGossipCases(t, cases)
}

func TestClashRoyaleDisabledCommandsStaySilent(t *testing.T) {
	var cases []gossipCase
	for _, tc := range []struct{ trigger, config string }{
		{"cr", `{"statsEnabled":"off"}`},
		{"crstats", `{"statsEnabled":"off"}`},
		{"crdecks", `{"decksEnabled":"off"}`},
		{"crranked", `{"rankedEnabled":"off"}`},
		{"crroad", `{"roadEnabled":"off"}`},
	} {
		cases = append(cases, gossipCase{name: tc.trigger, chat: clashChat("!"+tc.trigger, tc.config), silent: true})
	}
	runGossipCases(t, cases)
}
