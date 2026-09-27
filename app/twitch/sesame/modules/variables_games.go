// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package modules

import (
	"ItsBagelBot/app/twitch/sesame/engine"
	"ItsBagelBot/app/twitch/sesame/module"
	gossiprpc "ItsBagelBot/internal/domain/rpc/gossip"
	"strconv"
)

func valorantVariableReaders(d engine.Deps) map[string]variableRead {
	request := func(call statsCall[valorantConfig]) gossiprpc.Request {
		account, region, platform := valLookup(call, valScope{})
		return gossiprpc.Request{Account: account, Region: region, Platform: platform, IsPremium: call.Ctx.Regress.IsPremium()}
	}
	board := func(call statsCall[valorantConfig]) gossiprpc.Request {
		account, region, platform := valLookup(call, valScope{noBroadcasterFallback: true})
		return gossiprpc.Request{Account: account, Region: region, Platform: platform, IsPremium: call.Ctx.Regress.IsPremium()}
	}
	shop := func(call statsCall[valorantConfig]) gossiprpc.Request {
		return gossiprpc.Request{IsPremium: call.Ctx.Regress.IsPremium()}
	}
	return map[string]variableRead{
		"rank":    gossipVariables(d, valRoute("rank"), request, tokenValues(valRankTokens())),
		"matches": gossipVariables(d, valRoute("matches"), request, tokenValues(valMatchTokens())),
		"account": gossipVariables(d, valRoute("account"), request, tokenValues(valAccountTokens())),
		"board":   gossipVariables(d, valRoute("leaderboard"), board, tokenValues(valBoardTokens())),
		"shop":    gossipVariables(d, valRoute("shop"), shop, tokenValues(valShopTokens())),
	}
}

func clashVariableReaders(d engine.Deps) map[string]variableRead {
	request := variableAccount[clashroyaleConfig](false)
	return map[string]variableRead{
		"stats":  gossipVariables(d, clashRoute("stats"), request, tokenValues(clashStatsTokens())),
		"decks":  gossipVariables(d, clashRoute("decks"), request, tokenValues(clashDecksTokens())),
		"ranked": gossipVariables(d, clashRoute("ranked"), request, tokenValues(clashRankedTokens())),
		"road":   gossipVariables(d, clashRoute("trophy_road"), request, tokenValues(clashRoadTokens())),
	}
}

func codmVariableReaders(d engine.Deps) map[string]variableRead {
	request := func(call statsCall[codmConfig]) gossiprpc.Request {
		return accountRequest(call, codmProfileTarget(call))
	}
	return map[string]variableRead{"profile": gossipVariables(d, engine.GossipRoute{Provider: "codm", Endpoint: "profile"}, request, func(c *module.Context, r *gossiprpc.CODMProfileReply) module.StringPalette {
		var cfg codmConfig
		_ = c.Decode(&cfg)
		safeReply := *r
		safeReply.Player = codmProfileTarget(statsCall[codmConfig]{Ctx: c, Cfg: cfg}).Display
		return tokenValues(codmProfileTokens())(c, &safeReply)
	})}
}

func fortniteVariableReaders(d engine.Deps) map[string]variableRead {
	request := func(window string) func(statsCall[fortniteConfig]) gossiprpc.Request {
		return func(call statsCall[fortniteConfig]) gossiprpc.Request {
			req := variableAccount[fortniteConfig](false)(call)
			req.AccountType, req.TimeWindow = call.Cfg.AccountType, window
			return req
		}
	}
	session := func(call statsCall[fortniteConfig]) gossiprpc.Request {
		req := request("")(call)
		req.ChannelID = strconv.FormatUint(call.Ctx.BroadcasterID, 10)
		return req
	}
	shop := func(call statsCall[fortniteConfig]) gossiprpc.Request {
		return gossiprpc.Request{IsPremium: call.Ctx.Regress.IsPremium()}
	}
	return map[string]variableRead{
		"stats":  gossipVariables(d, fortniteRoute("stats"), request("lifetime"), tokenValues(fortniteStatsTokens())),
		"season": gossipVariables(d, fortniteRoute("stats"), request("season"), tokenValues(fortniteStatsTokens())),
		"session": gossipVariables(d, fortniteRoute("session"), session, func(_ *module.Context, r *gossiprpc.FortniteSessionReply) module.StringPalette {
			return module.StringPalette{"player": r.Player, "wins": i64(r.Wins), "matches": i64(r.Matches), "kills": i64(r.Kills), "kd": trimScore(r.KD), "winrate": trimScore(r.WinRate)}
		}),
		"store": gossipVariables(d, fortniteRoute("shop"), shop, func(c *module.Context, r *gossiprpc.FortniteShopReply) module.StringPalette {
			return module.StringPalette{"date": r.Date, "count": strconv.Itoa(r.Count), "items": formatShopEntries(c.Locale, r.Entries)}
		}),
	}
}
