// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package modules

import (
	"testing"

	gossiprpc "ItsBagelBot/internal/domain/rpc/gossip"
	"ItsBagelBot/pkg/bus"
)

func codmChat(text, config string) gossipChat {
	return gossipChat{module: "codm", text: text, config: config}
}

func TestCODMProfile(t *testing.T) {
	profile := map[string]any{"codm.profile": gossiprpc.CODMProfileReply{
		Player: "Streamer Mode Name", Level: 414, Rank: "Master I", RankClass: 5,
		Rating: 4590, Country: "CA", ShortID: "CA-4590",
	}}
	req := func(account string) *gossipCall {
		return &gossipCall{"codm.profile", gossiprpc.Request{Account: account}}
	}
	runGossipCases(t, []gossipCase{
		{name: "the default profile uses the exact linked nickname", chat: codmChat("!codm", `{"account":"Exact IGN"}`), replies: profile,
			exact: "Exact IGN · level 414 · MP Master I · 4590 rating · CA", call: req("Exact IGN")},
		{name: "the codmprofile alias keeps a typed nickname exact", chat: codmChat("!codmprofile   @MiXeD 名称  ", ""), replies: profile,
			exact: "@MiXeD 名称 · level 414 · MP Master I · 4590 rating · CA", call: req("@MiXeD 名称")},
		{name: "the codmrank alias keeps a typed nickname exact", chat: codmChat("!codmrank   @MiXeD 名称  ", ""), replies: profile,
			exact: "@MiXeD 名称 · level 414 · MP Master I · 4590 rating · CA", call: req("@MiXeD 名称")},
		{name: "a bare command uses the linked account", chat: codmChat("!codm", `{"account":"Linked IGN"}`), replies: profile, call: req("Linked IGN"),
			contains: []string{"Linked IGN"}},
		{name: "a missing linked account prints usage and never uses the twitch login", chat: codmChat("!codm", ""), replies: profile,
			contains: []string{"Usage"}, noCall: true},
		{name: "linkedOnly ignores the typed nickname", chat: codmChat("!codm Other Name", `{"account":"Linked IGN","linkedOnly":"on"}`), replies: profile,
			call: req("Linked IGN"), contains: []string{"Linked IGN"}},
		{name: "a disabled profile stays silent", chat: codmChat("!codm Other", `{"profileEnabled":"off"}`), silent: true},
		{name: "the profile message exposes every token", chat: codmChat("!codm uid-42", `{"profileMessage":"{player}|{level}|{rank}|{rankclass}|{rating}|{country}|{shortid}"}`),
			replies: profile, exact: "uid-42|414|Master I|5|4590|CA|CA-4590"},
		{name: "a reply error chats the typed nickname", chat: codmChat("!codm Ghost IGN", ""), err: bus.RPCReplyError{Message: "player not found"},
			exact: "Ghost IGN: player not found"},
	})
}
