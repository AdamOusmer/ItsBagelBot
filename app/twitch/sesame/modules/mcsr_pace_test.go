// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package modules

import (
	"testing"

	gossiprpc "ItsBagelBot/internal/domain/rpc/gossip"
)

func TestMcsrPaceCommands(t *testing.T) {
	session := func(r gossiprpc.PacemanSessionReply) map[string]any { return map[string]any{"paceman.session": r} }
	nethers := func(r gossiprpc.PacemanNethersReply) map[string]any { return map[string]any{"paceman.nethers": r} }
	lastFort := func(r gossiprpc.PacemanLastFortReply) map[string]any { return map[string]any{"paceman.lastfort": r} }
	linked := `{"account":"Feinberg","accountUuid":"` + testUUID + `"}`
	runGossipCases(t, []gossipCase{
		{name: "pace default template", chat: mcsrChat("!pace", ""),
			replies: session(gossiprpc.PacemanSessionReply{
				Player: "Feinberg", NetherCount: 3, Nether: "1:42", Bastion: "3:55",
				Fortress: "7:12", FirstPortal: "9:20", Stronghold: "12:05", End: "13:50", Finish: "0:00", NPH: 21.4,
			}),
			exact: "Feinberg this session: 3 nethers (avg 1:42) · bastion 3:55 · fortress 7:12 · fp 9:20 · 21.4 nph"},
		{name: "pace stays name keyed even when a uuid is stored", chat: mcsrChat("!pace", linked),
			replies: session(gossiprpc.PacemanSessionReply{Player: "Feinberg"}),
			call:    &gossipCall{"paceman.session", gossiprpc.Request{Account: "Feinberg"}}},
		{name: "pace accepts a typed player", chat: mcsrChat("!pace SomeoneElse", ""),
			replies: session(gossiprpc.PacemanSessionReply{Player: "SomeoneElse", Empty: true}),
			call:    &gossipCall{"paceman.session", gossiprpc.Request{Account: "SomeoneElse"}}},
		{name: "pace reports an empty session", chat: mcsrChat("!pace", ""),
			replies:  session(gossiprpc.PacemanSessionReply{Player: "Newbie", Empty: true}),
			contains: []string{"no pace tracked this session"}},
		{name: "pace toggled off stays silent", chat: mcsrChat("!pace", `{"paceEnabled":"off"}`), replies: session(gossiprpc.PacemanSessionReply{}), silent: true},
		{name: "nethers default template", chat: mcsrChat("!nethers", ""),
			replies: nethers(gossiprpc.PacemanNethersReply{Player: "Feinberg", Count: 3, Avg: "1:42", NPH: 21.4}),
			exact:   "Feinberg: 3 nethers this session (avg 1:42) · 21.4 nph"},
		{name: "nethers reports an empty session", chat: mcsrChat("!nethers", ""),
			replies:  nethers(gossiprpc.PacemanNethersReply{Player: "Newbie", Empty: true}),
			contains: []string{"no pace tracked this session"}},
		{name: "lastfort default template", chat: mcsrChat("!lastfort", ""),
			replies: lastFort(gossiprpc.PacemanLastFortReply{Player: "Feinberg", Nether: "1:30", Bastion: "2:45", Fortress: "5:00", AgoSeconds: 125}),
			exact:   "Feinberg last fort: nether 1:30 · bastion 2:45 · fortress 5:00 · fp - · sh - · 2m ago"},
		{name: "lastfort reports no recent fortress", chat: mcsrChat("!lastfort", ""),
			replies:  lastFort(gossiprpc.PacemanLastFortReply{Player: "Feinberg", Empty: true}),
			contains: []string{"no fortress pace tracked recently"}},
		{name: "lastfort toggled off stays silent", chat: mcsrChat("!lastfort", `{"lastFortEnabled":"off"}`), replies: lastFort(gossiprpc.PacemanLastFortReply{}), silent: true},
	})
}
