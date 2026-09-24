// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package modules

import (
	"fmt"
	"strings"
	"time"

	"ItsBagelBot/app/twitch/sesame/engine"
	"ItsBagelBot/app/twitch/sesame/module"
	gossiprpc "ItsBagelBot/internal/domain/rpc/gossip"
)

const valModuleName = "valorant"

const valCooldown = 10 * time.Second

const (
	defaultValRankTemplate    = "{player} · {tier} · {rr} RR ({lastchange}) · peak {peaktier}"
	defaultValMatchesTemplate = "{player}'s last {count}: {matches}"
	defaultValAccountTemplate = "{player} · account level {level}"
	defaultValBoardTemplate   = "{board}: {entries}"
	defaultValShopTemplate    = "Daily rotation ({count}): {items} · resets in {reset}"
)

const (
	valUnrankedText   = "has no competitive record this act"
	valNoMatchesText  = "has no recent competitive games"
	valEmptyBoardText = "leaderboard has no entries yet"
	valEmptyShopText  = "the daily rotation is empty today"
)

type valorantConfig struct {
	linkedAccountConfig

	Region   string `json:"region"`
	Platform string `json:"platform"`

	RankEnabled  string `json:"rankEnabled"`
	RankMessage  string `json:"rankMessage"`
	MatchEnabled string `json:"matchesEnabled"`
	MatchMessage string `json:"matchesMessage"`
	AcctEnabled  string `json:"accountEnabled"`
	AcctMessage  string `json:"accountMessage"`
	BoardEnabled string `json:"boardEnabled"`
	BoardMessage string `json:"boardMessage"`
	ShopEnabled  string `json:"shopEnabled"`
	ShopMessage  string `json:"shopMessage"`
}

func valRankSpecial(_ statsCall[valorantConfig], r *gossiprpc.ValorantRankReply) (string, bool) {
	if !r.Unranked {
		return "", false
	}
	return r.Player + " " + valUnrankedText, true
}

func valMatchSpecial(_ statsCall[valorantConfig], r *gossiprpc.ValorantMatchesReply) (string, bool) {
	if !r.Empty {
		return "", false
	}
	return r.Player + " " + valNoMatchesText, true
}

func valBoardSpecial(_ statsCall[valorantConfig], r *gossiprpc.ValorantLeaderboardReply) (string, bool) {
	if !r.Empty {
		return "", false
	}
	return r.Board + " " + valEmptyBoardText, true
}

func valShopSpecial(_ statsCall[valorantConfig], r *gossiprpc.ValorantShopReply) (string, bool) {
	if !r.Empty {
		return "", false
	}
	return valEmptyShopText, true
}

func valRoute(endpoint string) engine.GossipRoute {
	return engine.GossipRoute{Provider: "valorant", Endpoint: endpoint}
}

func newValRuns(d engine.Deps) valRuns {
	return valRuns{
		rank: valRun(d, valScope{}, externalCommand[valorantConfig, gossiprpc.ValorantRankReply]{
			route:    valRoute("rank"),
			enabled:  func(c valorantConfig) string { return c.RankEnabled },
			message:  func(c valorantConfig) string { return c.RankMessage },
			fallback: defaultValRankTemplate,
			tokens:   valRankTokens(),
			special:  valRankSpecial,
		}),
		matches: valRun(d, valScope{}, externalCommand[valorantConfig, gossiprpc.ValorantMatchesReply]{
			route:    valRoute("matches"),
			enabled:  func(c valorantConfig) string { return c.MatchEnabled },
			message:  func(c valorantConfig) string { return c.MatchMessage },
			fallback: defaultValMatchesTemplate,
			tokens:   valMatchTokens(),
			special:  valMatchSpecial,
		}),
		account: valRun(d, valScope{}, externalCommand[valorantConfig, gossiprpc.ValorantAccountReply]{
			route:    valRoute("account"),
			enabled:  func(c valorantConfig) string { return c.AcctEnabled },
			message:  func(c valorantConfig) string { return c.AcctMessage },
			fallback: defaultValAccountTemplate,
			tokens:   valAccountTokens(),
		}),
		board: valRun(d, valScope{noBroadcasterFallback: true}, externalCommand[valorantConfig, gossiprpc.ValorantLeaderboardReply]{
			route:    valRoute("leaderboard"),
			enabled:  func(c valorantConfig) string { return c.BoardEnabled },
			message:  func(c valorantConfig) string { return c.BoardMessage },
			fallback: defaultValBoardTemplate,
			tokens:   valBoardTokens(),
			special:  valBoardSpecial,
		}),
		shop: valRun(d, valScope{accountless: true}, externalCommand[valorantConfig, gossiprpc.ValorantShopReply]{
			route:    valRoute("shop"),
			enabled:  func(c valorantConfig) string { return c.ShopEnabled },
			message:  func(c valorantConfig) string { return c.ShopMessage },
			fallback: defaultValShopTemplate,
			tokens:   valShopTokens(),
			special:  valShopSpecial,
		}),
	}
}

func Valorant(d engine.Deps) module.Module {
	runs := newValRuns(d)

	m := module.NewModule(valModuleName, module.KindOptIn)
	m.Command("val").Everyone().Cooldown(valCooldown).
		Run(subDispatch(runs.rank, map[string]module.RunFunc{
			"match": runs.matches, "matches": runs.matches, "history": runs.matches,
			"account": runs.account, "who": runs.account,
			"lb": runs.board, "leaderboard": runs.board, "top": runs.board,
			"shop": runs.shop, "rotation": runs.shop,
			"rank": runs.rank, "standing": runs.rank,
		}))
	m.Command("valrank").Everyone().Cooldown(valCooldown).
		Run(runs.rank)
	m.Command("valmatches").Everyone().Cooldown(valCooldown).Aliases("valhistory").
		Run(runs.matches)
	m.Command("valaccount").Everyone().Cooldown(valCooldown).Aliases("valwho").
		Run(runs.account)
	m.Command("vallb").Everyone().Cooldown(valCooldown).Aliases("valleaderboard").
		Run(runs.board)
	m.Command("valshop").Everyone().Cooldown(valCooldown).Aliases("valrotation").
		Run(runs.shop)
	return m.Build()
}

type valRuns struct {
	rank, matches, account, board, shop module.RunFunc
}

type valScope struct {
	noBroadcasterFallback bool
	accountless           bool
}

func valRun[R any](d engine.Deps, scope valScope, cmd externalCommand[valorantConfig, R]) module.RunFunc {
	return statsHandler[valorantConfig, R]{
		d:       d,
		enabled: cmd.enabled,
		route:   cmd.route,
		target:  valTarget(scope),
		request: valRequest(scope),
		render:  cmd.render,
	}.run
}

func valTarget(scope valScope) func(statsCall[valorantConfig]) statsSubject {
	if scope.accountless {
		return fixedSubject[valorantConfig]("daily rotation")
	}
	return func(call statsCall[valorantConfig]) statsSubject {
		account, _, _ := valLookup(call, scope)
		return statsSubject{Account: account, Display: account}
	}
}

func valRequest(scope valScope) func(statsCall[valorantConfig], statsSubject) gossiprpc.Request {
	if scope.accountless {
		return accountRequest[valorantConfig]
	}
	return func(call statsCall[valorantConfig], _ statsSubject) gossiprpc.Request {
		req := gossiprpc.Request{IsPremium: call.Ctx.Regress.IsPremium()}
		req.Account, req.Region, req.Platform = valLookup(call, scope)
		return req
	}
}

func valLookup(call statsCall[valorantConfig], scope valScope) (account, region, platform string) {
	cfg := call.Cfg
	argAccount, region, platform := parseValArgs(call.Args)
	if explicitOn(cfg.LinkedOnly) {
		argAccount = ""
	}
	region, platform = orDefault(region, cfg.Region), orDefault(platform, cfg.Platform)
	if scope.noBroadcasterFallback {
		return orDefault(argAccount, cfg.Account), region, platform
	}
	account = resolveAccount(accountSources{Arg: argAccount, Linked: cfg.Account, BroadcasterLogin: call.Ctx.Env.BroadcasterUserLogin})
	return account, region, platform
}

func parseValArgs(args string) (account, region, platform string) {
	for _, f := range strings.Fields(args) {
		switch w := strings.ToLower(f); w {
		case "na", "eu", "ap", "kr", "br", "latam":
			region = w
		case "pc", "console":
			platform = w
		default:
			if account == "" {
				account = strings.TrimPrefix(f, "@")
			}
		}
	}
	return account, region, platform
}

func valRankTokens() module.TokenExpander[gossiprpc.ValorantRankReply] {
	type reply = gossiprpc.ValorantRankReply
	return module.TokenExpander[reply]{
		"player":     func(r *reply) string { return r.Player },
		"region":     func(r *reply) string { return r.Region },
		"tier":       func(r *reply) string { return r.Tier },
		"elo":        func(r *reply) string { return i64(int64(r.Elo)) },
		"rr":         func(r *reply) string { return i64(int64(r.RR)) },
		"lastchange": func(r *reply) string { return signed(r.LastChange) },
		"peaktier":   func(r *reply) string { return r.PeakTier },
		"placement":  func(r *reply) string { return i64(int64(r.Placement)) },
	}
}

func valMatchTokens() module.TokenExpander[gossiprpc.ValorantMatchesReply] {
	type reply = gossiprpc.ValorantMatchesReply
	return module.TokenExpander[reply]{
		"player": func(r *reply) string { return r.Player },
		"region": func(r *reply) string { return r.Region },
		"count":  func(r *reply) string { return i64(int64(len(r.Matches))) },
		"matches": func(r *reply) string {
			parts := make([]string, len(r.Matches))
			for i, m := range r.Matches {
				parts[i] = fmt.Sprintf("%s %d/%d/%d %s on %s", m.Agent, m.Kills, m.Deaths, m.Assists, m.Result, m.Map)
			}
			return strings.Join(parts, ", ")
		},
		"lastago": func(r *reply) string {
			if m := r.Matches; len(m) > 0 {
				return valAgo(m[0].AgoSeconds)
			}
			return ""
		},
	}
}

func valAccountTokens() module.TokenExpander[gossiprpc.ValorantAccountReply] {
	type reply = gossiprpc.ValorantAccountReply
	return module.TokenExpander[reply]{
		"player": func(r *reply) string { return r.Player },
		"puuid":  func(r *reply) string { return r.Puuid },
		"region": func(r *reply) string { return r.Region },
		"level":  func(r *reply) string { return i64(int64(r.AccountLevel)) },
		"card":   func(r *reply) string { return r.Card },
		"title":  func(r *reply) string { return r.Title },
	}
}

func valBoardTokens() module.TokenExpander[gossiprpc.ValorantLeaderboardReply] {
	type reply = gossiprpc.ValorantLeaderboardReply
	return module.TokenExpander[reply]{
		"player": func(r *reply) string { return r.Player },
		"board":  func(r *reply) string { return r.Board },
		"count":  func(r *reply) string { return i64(int64(len(r.Entries))) },
		"entries": func(r *reply) string {
			entries := r.Entries
			parts := make([]string, len(entries))
			for i, e := range entries {
				parts[i] = "#" + i64(int64(e.Rank)) + " " + e.Player + " (" + i64(int64(e.RR)) + " RR)"
			}
			return strings.Join(parts, ", ")
		},
	}
}

func valShopTokens() module.TokenExpander[gossiprpc.ValorantShopReply] {
	type reply = gossiprpc.ValorantShopReply
	return module.TokenExpander[reply]{
		"count": func(r *reply) string { return i64(int64(r.Count)) },
		"items": func(r *reply) string {
			items := r.Items
			parts := make([]string, len(items))
			for i, item := range items {
				parts[i] = item.Name + " (" + i64(item.Price) + " VP)"
			}
			return strings.Join(parts, ", ")
		},
		"reset": func(r *reply) string { return valResetIn(r.ResetUnix) },
	}
}

func valAgo(seconds int64) string {
	d := time.Duration(seconds) * time.Second
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd ago", int(d.Hours()/24))
	}
}

func valResetIn(unix int64) string {
	d := time.Until(time.Unix(unix, 0)).Round(time.Minute)
	if d < 0 {
		d = 0
	}
	h := int(d.Hours())
	m := int(d.Minutes()) % 60
	switch {
	case h > 0 && m > 0:
		return fmt.Sprintf("%dh %dm", h, m)
	case h > 0:
		return fmt.Sprintf("%dh", h)
	default:
		return fmt.Sprintf("%dm", m)
	}
}
