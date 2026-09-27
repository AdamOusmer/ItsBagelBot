// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package modules

import (
	"ItsBagelBot/app/twitch/sesame/engine"
	"ItsBagelBot/app/twitch/sesame/module"
	gossiprpc "ItsBagelBot/internal/domain/rpc/gossip"
	"strconv"
	"strings"
)

func urchinVariableReaders(d engine.Deps) map[string]variableRead {
	request := variableAccount[urchinConfig](true)
	readers := map[string]variableRead{
		"stats":  gossipVariables(d, urchinRoute("hypixel", "stats"), request, tokenValues(urchinStatsTokens())),
		"sniper": gossipVariables(d, urchinRoute("urchin", "sniper"), request, tokenValues(urchinSniperTokens())),
	}
	for _, window := range []string{"daily", "weekly", "monthly"} {
		readers[window] = gossipVariables(d, urchinRoute("urchin", window), request, tokenValues(urchinSessionTokens()))
	}
	for key, format := range map[string]func([]gossiprpc.UrchinTag) string{"tags": formatUrchinTags, "tagdescription": formatUrchinTagDescriptions} {
		readers[key] = gossipVariables(d, urchinRoute("urchin", "tags"), request, func(_ *module.Context, r *gossiprpc.UrchinTagsReply) module.StringPalette {
			return module.StringPalette{"player": r.Player, "tags": format(r.Tags), "tagcount": strconv.Itoa(len(r.Tags))}
		})
	}
	return readers
}

func mcsrVariableReaders(d engine.Deps) map[string]variableRead {
	request := variableAccount[mcsrConfig](true)
	nameRequest := variableAccount[mcsrConfig](false)
	session := func(call statsCall[mcsrConfig]) gossiprpc.Request {
		req := request(call)
		req.ChannelID = strconv.FormatUint(call.Ctx.BroadcasterID, 10)
		return req
	}
	board := func(call statsCall[mcsrConfig]) gossiprpc.Request {
		return gossiprpc.Request{Board: "elo", IsPremium: call.Ctx.Regress.IsPremium()}
	}
	pb := func(call statsCall[mcsrConfig]) gossiprpc.Request {
		req := nameRequest(call)
		req.TimeWindow = "all-time"
		return req
	}
	record := func(call statsCall[mcsrConfig]) gossiprpc.Request {
		fields := strings.Fields(call.Ctx.Env.Text)
		args := ""
		if len(fields) > 1 {
			args = strings.Join(fields[1:], " ")
		}
		args, season := parseMcsrSeason(args)
		a, b, _ := mcsrRecordAccounts(args, call.Cfg, call.Ctx)
		return gossiprpc.Request{Account: a, AccountB: b, Season: season, IsPremium: call.Ctx.Regress.IsPremium()}
	}
	return map[string]variableRead{
		"elo":       gossipVariables(d, mcsrRoute("user"), request, mcsrEloPalette),
		"session":   gossipVariables(d, mcsrRoute("session"), session, mcsrSessionPalette),
		"lastmatch": gossipVariables(d, mcsrRoute("last_match"), request, mcsrLastMatchPalette),
		"pace":      gossipVariables(d, pacemanRoute("session"), nameRequest, tokenValues(mcsrPaceTokens)),
		"nethers":   gossipVariables(d, pacemanRoute("nethers"), nameRequest, tokenValues(mcsrNethersTokens)),
		"lastfort":  gossipVariables(d, pacemanRoute("lastfort"), nameRequest, tokenValues(mcsrLastFortTokens)),
		"lb":        gossipVariables(d, mcsrRoute("leaderboard"), board, tokenValues(mcsrLbTokens)),
		"race":      gossipVariables(d, mcsrRoute("weekly_race"), request, tokenValues(mcsrRaceTokens)),
		"pb": gossipVariables(d, pacemanRoute("personal_best"), pb, func(c *module.Context, r *gossiprpc.PacemanPersonalBestReply) module.StringPalette {
			return module.StringPalette{"player": r.Player, "time": r.Time, "window": mcsrPbWindowLabel(c, r.Window)}
		}),
		"record": gossipVariables(d, mcsrRoute("versus"), record, tokenValues(mcsrRecordTokens)),
	}
}
