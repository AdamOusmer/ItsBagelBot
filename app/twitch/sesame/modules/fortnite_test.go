// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package modules

import (
	"strconv"
	"strings"
	"testing"

	gossiprpc "ItsBagelBot/internal/domain/rpc/gossip"
	"ItsBagelBot/pkg/bus"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func fortniteChat(text, config string) gossipChat {
	return gossipChat{module: "fortnite", text: text, config: config}
}

func fortniteStatsReply(window string) gossiprpc.FortniteStatsReply {
	return gossiprpc.FortniteStatsReply{
		Player:  "Ninja",
		Window:  window,
		Overall: gossiprpc.FortniteModeStats{Wins: 301, Matches: 6232, Kills: 21679, KD: 3.66, WinRate: 4.83},
		Solo:    gossiprpc.FortniteModeStats{Wins: 120, Matches: 2400, KD: 3.2},
		Duo:     gossiprpc.FortniteModeStats{Wins: 90, Matches: 1900, KD: 3.8},
		Squad:   gossiprpc.FortniteModeStats{Wins: 91, Matches: 1932, KD: 4.1},
	}
}

func fortniteStatsCall(account, accountType, window string) *gossipCall {
	return &gossipCall{"fortnite.stats", gossiprpc.Request{Account: account, AccountType: accountType, TimeWindow: window}}
}

func TestFortniteReplies(t *testing.T) {
	lifetime := map[string]any{"fortnite.stats": fortniteStatsReply("lifetime")}
	session := func(reply gossiprpc.FortniteSessionReply) map[string]any {
		return map[string]any{"fortnite.session": reply}
	}
	const linkedPSN = `{"account":"LinkedAcc","accountType":"psn"}`
	sessionCall := &gossipCall{"fortnite.session", gossiprpc.Request{Account: "Ninja", ChannelID: "2"}}
	runGossipCases(t, []gossipCase{
		{name: "fnstats default template", chat: fortniteChat("!fnstats", ""), replies: lifetime,
			exact: "Ninja all time: 301 wins in 6232 matches · 4.83% WR · 21679 kills · 3.66 K/D · solo 120W / duo 90W / squad 91W",
			call:  fortniteStatsCall("streamer", "", "lifetime")},
		{name: "fnseason default template", chat: fortniteChat("!fnseason", ""), replies: map[string]any{"fortnite.stats": fortniteStatsReply("season")},
			exact: "Ninja this season: 301 wins in 6232 matches · 4.83% WR · 21679 kills · 3.66 K/D · solo 120W / duo 90W / squad 91W",
			call:  fortniteStatsCall("streamer", "", "season")},
		{name: "the linked account and type pass through", chat: fortniteChat("!fnstats", linkedPSN), replies: lifetime,
			contains: []string{"Ninja"}, call: fortniteStatsCall("LinkedAcc", "psn", "lifetime")},
		{name: "a typed player beats the linked account but keeps its type", chat: fortniteChat("!fnstats @SomePlayer extra", linkedPSN), replies: lifetime,
			contains: []string{"Ninja"}, call: fortniteStatsCall("SomePlayer", "psn", "lifetime")},
		{name: "linkedOnly ignores the typed player", chat: fortniteChat("!fnstats @SomePlayer", `{"account":"LinkedAcc","accountType":"psn","linkedOnly":"on"}`), replies: lifetime,
			contains: []string{"Ninja"}, call: fortniteStatsCall("LinkedAcc", "psn", "lifetime")},
		{name: "linkedOnly without a linked account falls back to the broadcaster", chat: fortniteChat("!fnstats Other", `{"linkedOnly":"on"}`), replies: lifetime,
			contains: []string{"Ninja"}, call: fortniteStatsCall("streamer", "", "lifetime")},
		{name: "a reply error chats the typed player", chat: fortniteChat("!fnstats Ghosty", ""), err: bus.RPCReplyError{Message: "player not found"},
			exact: "Ghosty: player not found"},
		{name: "fnsession default template", chat: fortniteChat("!fnsession", `{"account":"Ninja"}`),
			replies: session(gossiprpc.FortniteSessionReply{Player: "Ninja", Wins: 3, Matches: 12, Kills: 48, KD: 5.33, WinRate: 25.0, HasSnapshot: true}),
			exact:   "Ninja this stream: 3 wins in 12 matches · 25% WR · 48 kills · 5.33 K/D", call: sessionCall},
		{name: "fnsession ignores a typed player", chat: fortniteChat("!fnsession SomeoneElse", `{"account":"Ninja"}`),
			replies:  session(gossiprpc.FortniteSessionReply{Player: "Ninja", HasSnapshot: true}),
			contains: []string{"Ninja"}, call: sessionCall},
		{name: "fnsession without a snapshot says tracking just started", chat: fortniteChat("!fnsession", ""),
			replies: session(gossiprpc.FortniteSessionReply{Player: "Ninja"}), contains: []string{"session tracking just started"}},
		{name: "fnstore default template", chat: fortniteChat("!fnstore", ""),
			replies: map[string]any{"fortnite.shop": gossiprpc.FortniteShopReply{
				Date: "2026-07-09", Count: 3,
				Entries: []gossiprpc.FortniteShopEntry{{Name: "Peely Bundle", Price: 2800}, {Name: "Renegade Raider", Price: 1200}, {Name: "Free Hat"}},
			}},
			exact: "Item Shop 2026-07-09: Peely Bundle (2800), Renegade Raider (1200), Free Hat"},
	})
}

func TestFortniteDispatch(t *testing.T) {
	shop := gossiprpc.FortniteShopReply{Date: "2026-07-10"}
	session := gossiprpc.FortniteSessionReply{Player: "Ninja", HasSnapshot: true}
	routes := []struct {
		name, text string
		replies    map[string]any
		call       *gossipCall
		route      string
	}{
		{"a bare fn is all time stats", "!fn", map[string]any{"fortnite.stats": fortniteStatsReply("lifetime")}, fortniteStatsCall("streamer", "", "lifetime"), ""},
		{"a player argument is stats", "!fn @SomePlayer extra", map[string]any{"fortnite.stats": fortniteStatsReply("lifetime")}, fortniteStatsCall("SomePlayer", "", "lifetime"), ""},
		{"season subcommand", "!fn season", map[string]any{"fortnite.stats": fortniteStatsReply("season")}, fortniteStatsCall("streamer", "", "season"), ""},
		{"season with a player", "!fn season OtherGuy", map[string]any{"fortnite.stats": fortniteStatsReply("season")}, fortniteStatsCall("OtherGuy", "", "season"), ""},
		{"session subcommand", "!fn session", map[string]any{"fortnite.session": session}, nil, "fortnite.session"},
		{"store subcommand", "!fn store", map[string]any{"fortnite.shop": shop}, nil, "fortnite.shop"},
		{"the shop alias is case insensitive", "!fn SHOP", map[string]any{"fortnite.shop": shop}, nil, "fortnite.shop"},
	}
	var cases []gossipCase
	for _, r := range routes {
		cases = append(cases, gossipCase{name: r.name, chat: fortniteChat(r.text, ""), replies: r.replies, call: r.call, route: r.route})
	}
	runGossipCases(t, cases)
}

func TestFortniteDisabledCommandsStaySilent(t *testing.T) {
	var cases []gossipCase
	for _, tc := range []struct{ trigger, config string }{
		{"fn", `{"statsEnabled":"off"}`},
		{"fnstats", `{"statsEnabled":"off"}`},
		{"fnseason", `{"seasonEnabled":"off"}`},
		{"fnsession", `{"sessionEnabled":"off"}`},
		{"fnstore", `{"storeEnabled":"off"}`},
	} {
		cases = append(cases, gossipCase{name: tc.trigger, chat: fortniteChat("!"+tc.trigger, tc.config), silent: true})
	}
	runGossipCases(t, cases)
}

func TestFortniteStoreBudgetsLongListings(t *testing.T) {
	var many []gossiprpc.FortniteShopEntry
	for i := range 60 {
		many = append(many, gossiprpc.FortniteShopEntry{Name: "Some Cosmetic Item " + strconv.Itoa(i), Price: 1200})
	}
	huge := gossiprpc.FortniteShopEntry{Name: strings.Repeat("x", fortniteShopBudget+50), Price: 100}
	cases := []struct {
		name     string
		entries  []gossiprpc.FortniteShopEntry
		contains []string
		maxLen   int
	}{
		{"an empty shop says so", nil, []string{"empty today"}, 0},
		{"a long listing is cut to the budget with a remainder", many, []string{"Item Shop 2026-07-09: Some Cosmetic Item 0 (1200), ", " more"}, len("Item Shop 2026-07-09: ") + fortniteShopBudget + len(" +99 more")},
		{"one oversized entry is still shown", []gossiprpc.FortniteShopEntry{huge, {Name: "Next", Price: 1}}, []string{huge.Name, "+1 more"}, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gw := &fakeGossip{replies: map[string]any{"fortnite.shop": gossiprpc.FortniteShopReply{Date: "2026-07-09", Count: len(tc.entries), Entries: tc.entries}}}
			out := runChat(t, Fortnite(gossipDeps(gw)), gameCtx(""), "!fnstore")
			require.Len(t, out, 1)
			for _, want := range tc.contains {
				assert.Contains(t, out[0].Text, want)
			}
			if tc.maxLen > 0 {
				assert.LessOrEqual(t, len(out[0].Text), tc.maxLen)
			}
		})
	}
}

func TestFortniteSessionSnapshots(t *testing.T) {
	runSnapshotCases(t, []snapshotCase{
		{name: "stream online stores the session baseline", module: "fortnite", event: "stream.online",
			config:  `{"account":"Ninja","accountType":"epic"}`,
			replies: map[string]any{"fortnite.session_start": gossiprpc.FortniteSnapshotReply{Player: "Ninja"}},
			call:    &gossipCall{"fortnite.session_start", gossiprpc.Request{Account: "Ninja", AccountType: "epic", ChannelID: "2"}}},
		{name: "stream online skips when sessions are off", module: "fortnite", event: "stream.online", config: `{"account":"Ninja","sessionEnabled":"off"}`},
		{name: "stream offline clears the channel baseline", module: "fortnite", event: "stream.offline",
			config:  `{"sessionEnabled":true,"account":"Ninja","accountType":"epic"}`,
			replies: map[string]any{"fortnite.session_end": gossiprpc.FortniteSnapshotReply{}},
			call:    &gossipCall{"fortnite.session_end", gossiprpc.Request{ChannelID: "2"}}},
		{name: "stream offline skips when sessions are off", module: "fortnite", event: "stream.offline", config: `{"account":"Ninja","sessionEnabled":"off"}`},
	})
}
