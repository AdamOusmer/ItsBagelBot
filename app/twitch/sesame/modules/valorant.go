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

// valModuleName is the ModuleView key; the console MODULE_CATALOG entry and
// the dashboard module page use the same id.
const valModuleName = "valorant"

// valCooldown is the shared per-command window; gossip caches every answer
// (two minutes on rank and matches, a day on the shop rotation), so this only
// shields chat from command spam, not the API.
const valCooldown = 10 * time.Second

// Default reply templates. The broadcaster customizes them per command on the
// module page; blank falls back to these.
const (
	defaultValRankTemplate    = "{player} · {tier} · {rr} RR ({lastchange}) · peak {peaktier}"
	defaultValMatchesTemplate = "{player}'s last {count}: {matches}"
	defaultValAccountTemplate = "{player} · account level {level}"
	defaultValBoardTemplate   = "{board}: {entries}"
	defaultValShopTemplate    = "Daily rotation ({count}): {items} · resets in {reset}"
)

// Special-case lines that replace their template entirely, because every
// numeric token would render zero (the same shape as !crranked's no-record
// answer).
const (
	valUnrankedText   = "has no competitive record this act"
	valNoMatchesText  = "has no recent competitive games"
	valEmptyBoardText = "leaderboard has no entries yet"
	valEmptyShopText  = "the daily rotation is empty today"
)

// valorantConfig is the module's dashboard configuration. Account is the
// linked Riot ID ("Name#Tag"); Region and Platform scope it ("na", "console",
// ...) — blank lets gossip detect the shard from the account and default to
// PC. Chat args override all three per call. The *Enabled toggles are stored
// "on"/"off" — empty means on, matching the alerts module's semantics — and
// each *Message is a customized template (blank = default).
type valorantConfig struct {
	// linkedAccountConfig carries account/accountUuid/linkedOnly: here the
	// account is a Riot ID ("Name#Tag"). Riot's own identifier is a puuid the
	// provider resolves per lookup, so accountUuid stays unset and is never
	// consulted (the resolution asks for it only under PreferUUID).
	linkedAccountConfig

	// Region and Platform scope the lookup ("na", "console", …); blank lets
	// gossip detect the shard from the account and default to PC. Chat args
	// override both per call.
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

// Valorant owns the Valorant chat commands backed by the gossip service. It
// is a named, opt-in module (KindOptIn): off by default, enabled on the
// dashboard, where the broadcaster links their Riot ID. Viewers can always
// target another player explicitly: "!val Frosty#EUW1".
//
// The command surface mirrors clashroyale's: one root with subcommands plus
// the squashed forms as direct triggers.
//
//	!val [id]            current competitive standing (also !valrank)
//	!val matches [id]    the last few competitive games (also !valmatches)
//	!val account [id]    who an ID resolves to, level/title (also !valaccount)
//	!val lb [region]     the regional top 10 (also !vallb)
//	!val shop            today's global skin rotation (also !valshop)
//
// An id is "Name#Tag"; any argument word naming a shard (na/eu/ap/kr/br/
// latam) or a ladder (pc/console) scopes the lookup wherever it sits, so
// "!val eu Frosty#EUW1" and "!val lb console ap" read naturally. Every
// account answer rides gossip's cache keyed on id+region+platform, and a
// region-less lookup auto-detects the shard once per day fleet-wide.
// The four empty-state overrides. Each replaces its command's template
// entirely when the reply carries no renderable content, because every numeric
// token would print zero.
func valRankSpecial(r *gossiprpc.ValorantRankReply) (string, bool) {
	if !r.Unranked {
		return "", false
	}
	return r.Player + " " + valUnrankedText, true
}

func valMatchSpecial(r *gossiprpc.ValorantMatchesReply) (string, bool) {
	if !r.Empty {
		return "", false
	}
	return r.Player + " " + valNoMatchesText, true
}

func valBoardSpecial(r *gossiprpc.ValorantLeaderboardReply) (string, bool) {
	if !r.Empty {
		return "", false
	}
	return r.Board + " " + valEmptyBoardText, true
}

func valShopSpecial(r *gossiprpc.ValorantShopReply) (string, bool) {
	if !r.Empty {
		return "", false
	}
	return valEmptyShopText, true
}

// valRoute names one Valorant gossip endpoint.
func valRoute(endpoint string) engine.GossipRoute {
	return engine.GossipRoute{Provider: "valorant", Endpoint: endpoint}
}

// newValRuns wires the five subcommand runners: each names its gossip
// endpoint, where its toggle and template live in the config blob, and which
// empty-state override applies.
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

// valRuns bundles the five subcommand runners so the root dispatcher takes one
// argument instead of a positional list that grows with every new view.
type valRuns struct {
	rank, matches, account, board, shop module.RunFunc
}

// valScope names the two places a Valorant lookup deviates from the shared
// account resolution. Everything else about these commands is the common
// shape, which is why they ride externalCommand and replace only the two
// strategies that scoping touches.
type valScope struct {
	// noBroadcasterFallback drops the "else the broadcaster's own login" step.
	// A bare "!vallb" is a regional top-N ask, not a lookup of the
	// broadcaster's standing, and their Twitch login is never a syntactically
	// valid Riot ID: the fallback would only mint "invalid riot id".
	noBroadcasterFallback bool
	// accountless: the daily rotation is global, so no account scopes it and
	// none is resolved.
	accountless bool
}

// valRun builds one command runner: the shared skeleton with Valorant's own
// scoping in place of the plain linked-account resolution.
func valRun[R any](d engine.Deps, scope valScope, cmd externalCommand[valorantConfig, R]) module.RunFunc {
	h := cmd.handler(d)
	h.target = valTarget(scope)
	h.request = valRequest(scope)
	return h.run
}

// valTarget names what a failure chats about: the resolved Riot ID, or the
// feature itself when nothing scopes the lookup.
func valTarget(scope valScope) func(statsCall[valorantConfig]) statsSubject {
	return func(call statsCall[valorantConfig]) statsSubject {
		if scope.accountless {
			return statsSubject{Display: "daily rotation"}
		}
		account, _, _ := valLookup(call, scope)
		return statsSubject{Account: account, Display: account}
	}
}

// valRequest builds the scoped lookup.
func valRequest(scope valScope) func(statsCall[valorantConfig], statsSubject) gossiprpc.Request {
	return func(call statsCall[valorantConfig], _ statsSubject) gossiprpc.Request {
		req := gossiprpc.Request{IsPremium: call.Ctx.Regress.IsPremium()}
		if scope.accountless {
			return req
		}
		req.Account, req.Region, req.Platform = valLookup(call, scope)
		return req
	}
}

// valLookup scopes one lookup: shard and ladder words peel off the typed args
// first and dashboard config fills whatever remains; the target account then
// resolves through the shared fallback chain unless the command opts out.
func valLookup(call statsCall[valorantConfig], scope valScope) (account, region, platform string) {
	cfg := call.Cfg
	argAccount, region, platform := parseValArgs(call.Args)
	if explicitOn(cfg.LinkedOnly) {
		// Linked-only drops the typed id; a shard or ladder word still
		// applies, since it only changes where the linked account is looked
		// up, not whose.
		argAccount = ""
	}
	region, platform = orDefault(region, cfg.Region), orDefault(platform, cfg.Platform)
	if scope.noBroadcasterFallback {
		return firstNonEmpty(argAccount, cfg.Account), region, platform
	}
	account = resolveAccount(accountSources{Arg: argAccount, Linked: cfg.Account, BroadcasterLogin: call.Ctx.Env.BroadcasterUserLogin})
	return account, region, platform
}

// parseValArgs splits a typed argument list into its scoping parts. Any word
// naming a shard or ladder sets the region/platform wherever it sits ("!val
// console ap" needs no account); the first remaining word is the account —
// always a username-shaped Riot ID ("Name#Tag"), never a numeric id. The shard
// set mirrors the provider's affinity table — adding one means touching both
// places, since sesame prefers to pre-scope than to round-trip an upstream
// rejection as a chat error.
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

// valRankTokens is the !valrank template palette over the gossip reply.
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

// valMatchTokens is the !valmatches palette: the games joined as one-liners
// (bounded by the upstream at five, so no truncation budget is needed) plus
// the age of the most recent one.
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

// valAccountTokens is the !valaccount palette.
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

// valBoardTokens is the !vallb palette: the rows joined as "#rank player (RR)"
// (bounded by the upstream at ten, so no truncation budget is needed).
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

// valShopTokens is the !valshop palette. The rotation is bounded by the game
// itself (a handful of direct-purchase skins per day), so the joined list fits
// a chat line without a truncation budget.
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

// valAgo renders a wall-clock age ("2h ago"); sub-minute reads as fresh
// because a completed match younger than that is still being played out in
// the client.
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

// valResetIn renders the countdown to the next shop flip, rounded to the
// minute so a template never prints "resets in 3h 59m" an hour early.
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
