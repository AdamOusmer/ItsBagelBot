// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package modules

import (
	"context"
	"testing"

	gossiprpc "ItsBagelBot/internal/domain/rpc/gossip"
	"ItsBagelBot/pkg/bus"
)

func TestUrchinCommands(t *testing.T) {
	sus := gossiprpc.UrchinTagsReply{
		Player: "Sus",
		Tags:   []gossiprpc.UrchinTag{{Type: "blatant_cheater", Reason: "bhop", AddedOn: 1720000000}, {Type: "sniper", AddedOn: 1720000000}},
	}
	linked := `{"account":"LinkedAcc","accountUuid":"` + testUUID + `"}`
	weekly := map[string]any{"urchin.weekly": gossiprpc.UrchinSessionReply{Player: "X"}}
	notFound := bus.RPCReplyError{Message: "player not found"}
	runGossipCases(t, []gossipCase{
		{name: "daily default template", chat: gossipChat{module: "urchin", text: "!daily"},
			replies: map[string]any{"urchin.daily": gossiprpc.UrchinSessionReply{
				Player: "Techno", Wins: 5, Losses: 2, FinalKills: 21, FinalDeaths: 3, BedsBroken: 9,
			}},
			exact: "Techno today: 5W 2L · 21 finals · 9 beds · 7.00 FKDR",
			call:  &gossipCall{"urchin.daily", gossiprpc.Request{Account: "streamer"}}},
		{name: "a linked account is used without an argument", chat: gossipChat{"urchin", "!weekly", `{"account":"LinkedAcc"}`}, replies: weekly,
			contains: []string{"X"}, call: &gossipCall{"urchin.weekly", gossiprpc.Request{Account: "LinkedAcc"}}},
		{name: "a typed player beats the linked account", chat: gossipChat{"urchin", "!weekly @SomePlayer extra words", `{"account":"LinkedAcc"}`}, replies: weekly,
			contains: []string{"X"}, call: &gossipCall{"urchin.weekly", gossiprpc.Request{Account: "SomePlayer"}}},
		{name: "a stored uuid beats the linked name", chat: gossipChat{"urchin", "!weekly", linked}, replies: weekly,
			contains: []string{"X"}, call: &gossipCall{"urchin.weekly", gossiprpc.Request{Account: testUUID}}},
		{name: "an error chats the linked name, not the uuid", chat: gossipChat{"urchin", "!daily", linked}, err: notFound,
			exact: "LinkedAcc: player not found"},
		{name: "a reply error chats back with the typed player", chat: gossipChat{module: "urchin", text: "!daily ghostplayer"}, err: notFound,
			exact: "ghostplayer: player not found"},
		{name: "an infrastructure error propagates and chats a retry", chat: gossipChat{module: "urchin", text: "!daily"}, err: context.DeadlineExceeded,
			contains: []string{"try again in a moment"}, wantErr: true},
		{name: "a toggled off command stays silent", chat: gossipChat{"urchin", "!monthly", `{"monthlyEnabled":"off"}`},
			replies: map[string]any{"urchin.monthly": gossiprpc.UrchinSessionReply{Player: "X"}}, silent: true},
		{name: "a custom template fills the stats tokens", chat: gossipChat{"urchin", "!bwstats", `{"statsMessage":"{player} is {stars} stars with {wlr} WLR"}`},
			replies: map[string]any{"hypixel.stats": gossiprpc.HypixelStatsReply{Player: "Techno", Stars: 402, Wins: 1000, Losses: 100}},
			exact:   "Techno is 402 stars with 10.00 WLR"},
		{name: "tags list each tag with its date", chat: gossipChat{module: "urchin", text: "!tag"}, replies: map[string]any{"urchin.tags": sus},
			exact: "Sus: Blatant Cheater (added Jul 3, 2024), Sniper (added Jul 3, 2024)"},
		{name: "a clean player has no tags", chat: gossipChat{module: "urchin", text: "!tag"},
			replies: map[string]any{"urchin.tags": gossiprpc.UrchinTagsReply{Player: "Clean"}}, exact: "Clean: No tags"},
		{name: "tagdescription adds the reasons", chat: gossipChat{module: "urchin", text: "!tagdescription"}, replies: map[string]any{"urchin.tags": sus},
			exact: "Sus: Blatant Cheater (bhop - added Jul 3, 2024), Sniper (added Jul 3, 2024)"},
		{name: "sniper reports the score", chat: gossipChat{module: "urchin", text: "!sniper"},
			replies: map[string]any{"urchin.sniper": gossiprpc.UrchinSniperReply{Player: "Aim", Score: 7.5, Mode: "warn", TagCount: 1}},
			exact:   "Aim urchin score: 7.5"},
	})
}

func urchinChat(text string) gossipChat { return gossipChat{module: "urchin", text: text} }
