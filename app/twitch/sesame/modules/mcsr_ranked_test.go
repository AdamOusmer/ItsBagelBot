// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package modules

import (
	"testing"

	gossiprpc "ItsBagelBot/internal/domain/rpc/gossip"
	"ItsBagelBot/pkg/bus"
)

func mcsrCall(route string, req gossiprpc.Request) *gossipCall { return &gossipCall{route, req} }

func TestMcsrEloCommand(t *testing.T) {
	user := func(r gossiprpc.McsrUserReply) map[string]any { return map[string]any{"mcsr.user": r} }
	feinberg := user(gossiprpc.McsrUserReply{Nickname: "Feinberg", Elo: 1650, Rank: 12, Wins: 40, Loses: 20})
	linked := `{"account":"Feinberg","accountUuid":"` + testUUID + `"}`
	runGossipCases(t, []gossipCase{
		{name: "elo default template", chat: mcsrChat("!elo", ""), replies: feinberg,
			exact: "Feinberg: 1650 elo · rank #12 · 40W 20L this season", call: mcsrCall("mcsr.user", gossiprpc.Request{Account: "streamer"})},
		{name: "elo unrated shows dashes", chat: mcsrChat("!elo", ""), replies: user(gossiprpc.McsrUserReply{Nickname: "Newbie", Elo: -1, Rank: -1}),
			contains: []string{"unrated elo", "#-"}},
		{name: "elo uses the linked uuid", chat: mcsrChat("!elo", linked), replies: feinberg,
			contains: []string{"Feinberg"}, call: mcsrCall("mcsr.user", gossiprpc.Request{Account: testUUID})},
		{name: "elo error chats the linked name, not the uuid", chat: mcsrChat("!elo", linked), err: bus.RPCReplyError{Message: "player not found"},
			exact: "Feinberg: player not found"},
		{name: "a season token reaches the request and not the player", chat: mcsrChat("!elo Feinberg season:5", ""), replies: feinberg,
			contains: []string{"Feinberg"}, call: mcsrCall("mcsr.user", gossiprpc.Request{Account: "Feinberg", Season: 5})},
		{name: "a leading season token is stripped from the player", chat: mcsrChat("!elo season:11 Feinberg", ""), replies: feinberg,
			contains: []string{"Feinberg"}, call: mcsrCall("mcsr.user", gossiprpc.Request{Account: "Feinberg", Season: 11})},
		{name: "a season token alone keeps the default player", chat: mcsrChat("!elo season:11", ""), replies: feinberg,
			contains: []string{"Feinberg"}, call: mcsrCall("mcsr.user", gossiprpc.Request{Account: "streamer", Season: 11})},
		{name: "an invalid season number stays part of the arguments", chat: mcsrChat("!elo season:abc Feinberg", ""), replies: feinberg,
			contains: []string{"Feinberg"}, call: mcsrCall("mcsr.user", gossiprpc.Request{Account: "season:abc"})},
		{name: "a zero season stays part of the arguments", chat: mcsrChat("!elo season:0 Feinberg", ""), replies: feinberg,
			contains: []string{"Feinberg"}, call: mcsrCall("mcsr.user", gossiprpc.Request{Account: "season:0"})},
		{name: "the season prefix is case insensitive", chat: mcsrChat("!elo Feinberg SEASON:9", ""), replies: feinberg,
			contains: []string{"Feinberg"}, call: mcsrCall("mcsr.user", gossiprpc.Request{Account: "Feinberg", Season: 9})},
		{name: "elo renders the namespaced saved template", chat: mcsrChat("!elo", `{"eloMessage":"{mcsr:player}: {mcsr:elo} elo, #{mcsr:rank}, {mcsr:draws} draws"}`),
			replies: user(gossiprpc.McsrUserReply{Nickname: "Feinberg", Elo: 1650, Rank: 12, Wins: 40, Loses: 20, Played: 63}),
			exact:   "Feinberg: 1650 elo, #12, 3 draws"},
	})
}

func TestMcsrSessionCommand(t *testing.T) {
	session := func(r gossiprpc.McsrSessionReply) map[string]any { return map[string]any{"mcsr.session": r} }
	feinberg := session(gossiprpc.McsrSessionReply{Nickname: "Feinberg", Elo: 1660, EloChange: 24, Wins: 3, Loses: 1, Played: 4, HasSnapshot: true})
	const account = `{"account":"Feinberg"}`
	sessionCall := mcsrCall("mcsr.session", gossiprpc.Request{Account: "Feinberg", ChannelID: "2"})
	runGossipCases(t, []gossipCase{
		{name: "session reports the gain since the baseline", chat: mcsrChat("!session", account), replies: feinberg,
			exact: "Feinberg this stream: +24 elo (1660 now) · 3W 1L 0D in 4 matches", call: sessionCall},
		{name: "session draws fill the gap", chat: mcsrChat("!session", `{"account":"LawnMobius"}`),
			replies: session(gossiprpc.McsrSessionReply{Nickname: "LawnMobius", Elo: 1568, EloChange: -13, Wins: 3, Loses: 4, Played: 8, HasSnapshot: true}),
			exact:   "LawnMobius this stream: -13 elo (1568 now) · 3W 4L 1D in 8 matches"},
		{name: "session draws are never negative", chat: mcsrChat("!session", account),
			replies:  session(gossiprpc.McsrSessionReply{Nickname: "Feinberg", Wins: 3, Loses: 1, Played: 2, HasSnapshot: true}),
			contains: []string{"3W 1L 0D in 2 matches"}},
		{name: "session ignores a typed player", chat: mcsrChat("!session SomeoneElse", account), replies: feinberg, contains: []string{"Feinberg"}, call: sessionCall},
		{name: "session without a snapshot says tracking just started", chat: mcsrChat("!session", ""),
			replies:  session(gossiprpc.McsrSessionReply{Nickname: "Feinberg", Elo: 1650}),
			contains: []string{"session tracking just started"}},
		{name: "session toggled off stays silent", chat: mcsrChat("!session", `{"sessionEnabled":"off"}`), replies: session(gossiprpc.McsrSessionReply{}), silent: true},
		{name: "an empty saved template falls back to the default", chat: mcsrChat("!session", `{"account":"Feinberg","sessionMessage":""}`), replies: feinberg,
			exact: "Feinberg this stream: +24 elo (1660 now) · 3W 1L 0D in 4 matches"},
		{name: "the legacy saved template is upgraded to the default", chat: mcsrChat("!session", `{"sessionMessage":`+quote(legacyMcsrSessionTemplate)+`}`), replies: feinberg,
			exact: "Feinberg this stream: +24 elo (1660 now) · 3W 1L 0D in 4 matches"},
		{name: "a custom saved template is kept", chat: mcsrChat("!session", `{"sessionMessage":"{wins}-{losses}"}`), replies: feinberg, exact: "3-1"},
		{name: "an edited legacy template is kept as written", chat: mcsrChat("!session", `{"sessionMessage":`+quote(legacyMcsrSessionTemplate+"!")+`}`), replies: feinberg,
			exact: "Feinberg this stream: +24 elo (1660 now) · 3W 1L in 4 matches!"},
	})
}

func TestMcsrLastMatchCommand(t *testing.T) {
	last := func(r gossiprpc.McsrLastMatchReply) map[string]any { return map[string]any{"mcsr.last_match": r} }
	runGossipCases(t, []gossipCase{
		{name: "lastmatch default template", chat: mcsrChat("!lastmatch", ""),
			replies: last(gossiprpc.McsrLastMatchReply{
				Player: "Feinberg", Opponent: "lowk3y_", Result: "win", Time: "11:03.135",
				Seed: "Desert Temple", Structure: "Treasure", EloChange: 21, AgoSeconds: 125,
			}),
			exact: "Feinberg vs lowk3y_: won · 11:03.135 · Desert Temple Treasure · +21 elo · 2m ago"},
		{name: "a forfeit shows a dash for the missing time", chat: mcsrChat("!lastmatch", ""),
			replies:  last(gossiprpc.McsrLastMatchReply{Player: "Feinberg", Opponent: "lowk3y_", Result: "loss", Forfeited: true}),
			contains: []string{"lost (forfeit)", "-"}},
		{name: "a decayed match is labelled", chat: mcsrChat("!lastmatch", ""),
			replies:  last(gossiprpc.McsrLastMatchReply{Player: "Feinberg", Opponent: "lowk3y_", Result: "win", Decayed: true}),
			contains: []string{"won (decay)"}},
		{name: "no matches yet", chat: mcsrChat("!lastmatch", ""),
			replies: last(gossiprpc.McsrLastMatchReply{Player: "Newbie", Empty: true}), contains: []string{"no matches found yet"}},
		{name: "a season token never leaks into the player name", chat: mcsrChat("!lastmatch Feinberg season:11", ""),
			replies: last(gossiprpc.McsrLastMatchReply{Player: "Feinberg", Empty: true}),
			call:    mcsrCall("mcsr.last_match", gossiprpc.Request{Account: "Feinberg", Season: 11}), contains: []string{"Feinberg"}},
		{name: "lastmatch toggled off stays silent", chat: mcsrChat("!lastmatch", `{"lastMatchEnabled":"off"}`), replies: last(gossiprpc.McsrLastMatchReply{}), silent: true},
	})
}

func TestMcsrRecordCommand(t *testing.T) {
	versus := map[string]any{"mcsr.versus": gossiprpc.McsrRecordReply{PlayerA: "Feinberg", PlayerB: "lowk3y_", WinsA: 20, WinsB: 14, Played: 34}}
	const text = "Feinberg 20 - 14 lowk3y_ · 34 played"
	record := func(a, b string) *gossipCall {
		return mcsrCall("mcsr.versus", gossiprpc.Request{Account: a, AccountB: b})
	}
	runGossipCases(t, []gossipCase{
		{name: "record of two typed players", chat: mcsrChat("!record Feinberg lowk3y_", ""), replies: versus, exact: text, call: record("Feinberg", "lowk3y_")},
		{name: "a single typed player is paired with the linked account", chat: mcsrChat("!record lowk3y_", `{"account":"Feinberg"}`), replies: versus,
			exact: text, call: record("Feinberg", "lowk3y_")},
		{name: "a single typed player is paired with the linked uuid", chat: mcsrChat("!record lowk3y_", `{"account":"Feinberg","accountUuid":"`+testUUID+`"}`), replies: versus,
			exact: text, call: record(testUUID, "lowk3y_")},
		{name: "linkedOnly pins the first side to the linked account", chat: mcsrChat("!record lowk3y_ Couriway", `{"account":"Feinberg","linkedOnly":"on"}`), replies: versus,
			exact: text, call: record("Feinberg", "lowk3y_")},
		{name: "no typed player prints usage without an upstream call", chat: mcsrChat("!record", ""), replies: versus, contains: []string{"Usage"}, noCall: true},
		{name: "record toggled off stays silent", chat: mcsrChat("!record a b", `{"recordEnabled":"off"}`), replies: versus, silent: true},
	})
}

func TestMcsrLeaderboardAndRace(t *testing.T) {
	board := func(r gossiprpc.McsrLeaderboardReply) map[string]any { return map[string]any{"mcsr.leaderboard": r} }
	race := func(r gossiprpc.McsrWeeklyRaceReply) map[string]any { return map[string]any{"mcsr.weekly_race": r} }
	runGossipCases(t, []gossipCase{
		{name: "lb default shows the elo board", chat: mcsrChat("!lb", ""),
			replies: board(gossiprpc.McsrLeaderboardReply{Board: "elo", Entries: []gossiprpc.McsrLeaderboardEntry{{Rank: 1, Name: "A", Value: "2400"}, {Rank: 2, Name: "B", Value: "2380"}}}),
			exact:   "Elo: #1 A 2400 · #2 B 2380", call: mcsrCall("mcsr.leaderboard", gossiprpc.Request{})},
		{name: "lb takes a board, a predicted flag and a country", chat: mcsrChat("!lb phase predicted country:us", ""),
			replies: board(gossiprpc.McsrLeaderboardReply{Board: "phase", Entries: []gossiprpc.McsrLeaderboardEntry{{Rank: 1, Name: "A", Value: "80"}}}),
			exact:   "Phase: #1 A 80", call: mcsrCall("mcsr.leaderboard", gossiprpc.Request{Board: "phase", Predicted: true, Country: "us"})},
		{name: "lb takes a season token", chat: mcsrChat("!lb season:11", ""), replies: board(gossiprpc.McsrLeaderboardReply{Board: "elo", Empty: true}),
			contains: []string{"nobody on this leaderboard yet"}, call: mcsrCall("mcsr.leaderboard", gossiprpc.Request{Season: 11})},
		{name: "lb reports an empty board", chat: mcsrChat("!lb record", ""), replies: board(gossiprpc.McsrLeaderboardReply{Board: "record", Empty: true}),
			contains: []string{"nobody on this leaderboard yet"}},
		{name: "lb toggled off stays silent", chat: mcsrChat("!lb", `{"lbEnabled":"off"}`), replies: board(gossiprpc.McsrLeaderboardReply{}), silent: true},
		{name: "race default template", chat: mcsrChat("!race", ""),
			replies: race(gossiprpc.McsrWeeklyRaceReply{LeaderName: "gharfyy", LeaderTime: "2:27.374", Player: "Feinberg", PlayerTime: "2:40.000", PlayerRank: 2, HasPlayer: true}),
			exact:   "#1 gharfyy (2:27.374) · Feinberg: 2:40.000 (#2)"},
		{name: "race without a player time says so", chat: mcsrChat("!race", ""),
			replies:  race(gossiprpc.McsrWeeklyRaceReply{LeaderName: "gharfyy", LeaderTime: "2:27.374", Player: "Newbie"}),
			contains: []string{"#1 gharfyy (2:27.374)", "no time in this week's race yet"}},
		{name: "race reports no submissions", chat: mcsrChat("!race", ""), replies: race(gossiprpc.McsrWeeklyRaceReply{Empty: true}),
			contains: []string{"no times submitted for this week's race yet"}},
		{name: "race toggled off stays silent", chat: mcsrChat("!race", `{"raceEnabled":"off"}`), replies: race(gossiprpc.McsrWeeklyRaceReply{}), silent: true},
	})
}

func TestMcsrPbCommand(t *testing.T) {
	pb := func(player, window, time string, empty bool) map[string]any {
		return map[string]any{"paceman.personal_best": gossiprpc.PacemanPersonalBestReply{Player: player, Window: window, Time: time, Empty: empty}}
	}
	ranked := func(r gossiprpc.McsrUserReply) map[string]any { return map[string]any{"mcsr.user": r} }
	const account = `{"account":"Feinberg"}`
	pbCall := func(account, window string) *gossipCall {
		return mcsrCall("paceman.personal_best", gossiprpc.Request{Account: account, TimeWindow: window})
	}
	runGossipCases(t, []gossipCase{
		{name: "a bare pb is the all time best", chat: mcsrChat("!pb", account), replies: pb("Feinberg", "all-time", "6:10.012", false),
			exact: "Feinberg: 6:10.012 (all-time PB)", call: pbCall("Feinberg", "")},
		{name: "pb takes a daily window", chat: mcsrChat("!pb daily", account), replies: pb("Feinberg", "daily", "6:40.123", false),
			exact: "Feinberg: 6:40.123 (daily PB)", call: pbCall("Feinberg", "daily")},
		{name: "pb takes a weekly window", chat: mcsrChat("!pb weekly", account), replies: pb("Feinberg", "weekly", "6:40.123", false),
			exact: "Feinberg: 6:40.123 (weekly PB)", call: pbCall("Feinberg", "weekly")},
		{name: "pb takes a monthly window", chat: mcsrChat("!pb monthly", account), replies: pb("Feinberg", "monthly", "6:40.123", false),
			exact: "Feinberg: 6:40.123 (monthly PB)", call: pbCall("Feinberg", "monthly")},
		{name: "the window keyword is case insensitive", chat: mcsrChat("!pb DAILY", account), replies: pb("Feinberg", "daily", "6:40.123", false),
			exact: "Feinberg: 6:40.123 (daily PB)", call: pbCall("Feinberg", "daily")},
		{name: "an unrecognized first word is a player name", chat: mcsrChat("!pb monthlyish", account), replies: pb("monthlyish", "all-time", "5:59.000", false),
			contains: []string{"monthlyish"}, call: pbCall("monthlyish", "")},
		{name: "a bare name is another player's all time best", chat: mcsrChat("!pb lowk3y_", account), replies: pb("lowk3y_", "all-time", "5:59.000", false),
			contains: []string{"lowk3y_"}, call: pbCall("lowk3y_", "")},
		{name: "a window and a name combine", chat: mcsrChat("!pb weekly lowk3y_", account), replies: pb("lowk3y_", "weekly", "6:01.500", false),
			contains: []string{"lowk3y_"}, call: pbCall("lowk3y_", "weekly")},
		{name: "no personal best in the window", chat: mcsrChat("!pb daily", `{"account":"Newbie"}`), replies: pb("Newbie", "daily", "", true),
			exact: "Newbie: no personal best yet (daily)"},
		{name: "pb stays name keyed for paceman even with a stored uuid", chat: mcsrChat("!pb daily", `{"account":"Feinberg","accountUuid":"`+testUUID+`"}`),
			replies: pb("Feinberg", "daily", "6:40.123", false), contains: []string{"Feinberg"}, call: pbCall("Feinberg", "daily")},
		{name: "ranked pb reads the ranked profile", chat: mcsrChat("!pb ranked", account), replies: ranked(gossiprpc.McsrUserReply{Nickname: "Feinberg", Elo: 1650, BestTimeMS: 595036}),
			exact: "Feinberg: 9:55.036 (ranked PB)", route: "mcsr.user"},
		{name: "ranked pb uses the linked uuid", chat: mcsrChat("!pb ranked", `{"account":"Feinberg","accountUuid":"`+testUUID+`"}`),
			replies: ranked(gossiprpc.McsrUserReply{Nickname: "Feinberg", BestTimeMS: 595036}), contains: []string{"Feinberg"},
			call: mcsrCall("mcsr.user", gossiprpc.Request{Account: testUUID})},
		{name: "an unrated ranked pb says none yet", chat: mcsrChat("!pb ranked", `{"account":"Newbie"}`), replies: ranked(gossiprpc.McsrUserReply{Nickname: "Newbie", Elo: -1}),
			exact: "Newbie: no personal best yet (ranked)"},
		{name: "pb toggled off stays silent", chat: mcsrChat("!pb", `{"pbEnabled":"off"}`), replies: pb("Feinberg", "", "6:10.012", false), silent: true},
		{name: "an upstream rejection chats the typed player", chat: mcsrChat("!pb ghostplayer", ""), err: bus.RPCReplyError{Message: "player not found"},
			exact: "ghostplayer: player not found"},
	})
}
