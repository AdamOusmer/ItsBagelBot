// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package modules

import (
	"context"
	"fmt"
	"strings"
	"time"

	"ItsBagelBot/app/twitch/sesame/engine"
	"ItsBagelBot/app/twitch/sesame/module"
	"ItsBagelBot/internal/domain/outgress"
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
	Account  string `json:"account"`
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
// entirely when the reply carries no renderable content, because every
// numeric token would print zero.
func valRankSpecial(reply any) (string, bool) {
	r, ok := reply.(*gossiprpc.ValorantRankReply)
	if !ok || !r.Unranked {
		return "", false
	}
	return r.Player + " " + valUnrankedText, true
}

func valMatchSpecial(reply any) (string, bool) {
	r, ok := reply.(*gossiprpc.ValorantMatchesReply)
	if !ok || !r.Empty {
		return "", false
	}
	return r.Player + " " + valNoMatchesText, true
}

func valBoardSpecial(reply any) (string, bool) {
	r, ok := reply.(*gossiprpc.ValorantLeaderboardReply)
	if !ok || !r.Empty {
		return "", false
	}
	return r.Board + " " + valEmptyBoardText, true
}

func valShopSpecial(reply any) (string, bool) {
	r, ok := reply.(*gossiprpc.ValorantShopReply)
	if !ok || !r.Empty {
		return "", false
	}
	return valEmptyShopText, true
}

// newValRuns wires the five subcommand runners: each names its gossip
// endpoint, where its toggle and template live in the config blob, and which
// empty-state override applies.
func newValRuns(d engine.Deps) valRuns {
	return valRuns{
		rank: valRun(d, valCommand{
			endpoint: "rank",
			enabled:  func(c valorantConfig) string { return c.RankEnabled },
			message:  func(c valorantConfig) string { return c.RankMessage },
			fallback: defaultValRankTemplate,
			newReply: func() any { return &gossiprpc.ValorantRankReply{} },
			tokens:   valRankTokens(),
			special:  valRankSpecial,
		}),
		matches: valRun(d, valCommand{
			endpoint: "matches",
			enabled:  func(c valorantConfig) string { return c.MatchEnabled },
			message:  func(c valorantConfig) string { return c.MatchMessage },
			fallback: defaultValMatchesTemplate,
			newReply: func() any { return &gossiprpc.ValorantMatchesReply{} },
			tokens:   valMatchTokens(),
			special:  valMatchSpecial,
		}),
		account: valRun(d, valCommand{
			endpoint: "account",
			enabled:  func(c valorantConfig) string { return c.AcctEnabled },
			message:  func(c valorantConfig) string { return c.AcctMessage },
			fallback: defaultValAccountTemplate,
			newReply: func() any { return &gossiprpc.ValorantAccountReply{} },
			tokens:   valAccountTokens(),
		}),
		board: valRun(d, valCommand{
			endpoint: "leaderboard",
			enabled:  func(c valorantConfig) string { return c.BoardEnabled },
			message:  func(c valorantConfig) string { return c.BoardMessage },
			fallback: defaultValBoardTemplate,
			newReply: func() any { return &gossiprpc.ValorantLeaderboardReply{} },
			tokens:   valBoardTokens(),
			special:  valBoardSpecial,
			// A bare "!vallb" is a regional top-N ask, not a lookup of the
			// broadcaster's own standing: falling back to their Twitch login
			// (never a syntactically valid Riot ID) would only produce
			// "invalid riot id" instead of the board they asked for.
			noBroadcasterFallback: true,
		}),
		shop: valRun(d, valCommand{
			endpoint: "shop",
			enabled:  func(c valorantConfig) string { return c.ShopEnabled },
			message:  func(c valorantConfig) string { return c.ShopMessage },
			fallback: defaultValShopTemplate,
			newReply: func() any { return &gossiprpc.ValorantShopReply{} },
			tokens:   valShopTokens(),
			special:  valShopSpecial,
			// The rotation is global: no account scopes it, so none is resolved.
			accountless: true,
		}),
	}
}

func Valorant(d engine.Deps) module.Module {
	runs := newValRuns(d)

	m := module.NewModule(valModuleName, module.KindOptIn)
	m.Command("val").Everyone().Cooldown(valCooldown).
		Run(valDispatchRun(runs))
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

// valCommand names one command's wiring: the gossip endpoint it queries,
// where its toggle and template live in the config blob, an empty reply value
// Call decodes into, an optional post-decode override (the three empty-state
// answers replace their template entirely), and two account-resolution
// deviations shared by !vallb and !valshop.
type valCommand struct {
	endpoint string
	enabled  func(valorantConfig) string
	message  func(valorantConfig) string
	fallback string
	newReply func() any
	tokens   map[string]func(any) string
	special  func(reply any) (string, bool)

	noBroadcasterFallback bool
	accountless           bool
}

// valRun builds one command runner: scope the request, call the endpoint,
// expand the template over the reply. Tokens are declared against `any` so
// one runner serves five reply types; each palette casts back.
func valRun(d engine.Deps, cmd valCommand) module.RunFunc {
	return func(ctx context.Context, c *module.Context, args string, emit module.Emit) error {
		var cfg valorantConfig
		_ = c.Decode(&cfg)
		if !alertOn(cmd.enabled(cfg)) || d.Gossip == nil {
			return nil
		}

		req := valRequest(cmd, cfg, c, args)
		reply := cmd.newReply()
		if err := d.Gossip.Call(ctx, engine.GossipRoute{Provider: "valorant", Endpoint: cmd.endpoint}, req, reply); err != nil {
			if chatReplyError(c, emit, valSubject(cmd, req), err) {
				return nil
			}
			return err
		}
		if text, ok := valSpecial(cmd.special, reply); ok {
			emit(&module.Output{Type: outgress.TypeChat, BroadcasterID: c.Env.BroadcasterUserID, Text: text})
			return nil
		}

		msg := module.ExpandString(orDefault(cmd.message(cfg), cmd.fallback), func(key string) (string, bool) {
			if field, ok := cmd.tokens[key]; ok {
				return field(reply), true
			}
			return module.ParseDynamic(key)
		})
		emit(&module.Output{Type: outgress.TypeChat, BroadcasterID: c.Env.BroadcasterUserID, Text: msg})
		return nil
	}
}

// valRequest scopes one lookup: shard and ladder words peel off the typed args
// first and dashboard config fills whatever remains; the target account then
// resolves through the shared fallback chain — unless the command opts out,
// because a board ask without an id is a regional top-N (the login fallback
// would only mint "invalid riot id") and the shop has no target at all.
func valRequest(cmd valCommand, cfg valorantConfig, c *module.Context, args string) gossiprpc.Request {
	req := gossiprpc.Request{IsPremium: c.Regress.IsPremium()}
	if cmd.accountless {
		return req
	}
	argAccount, argRegion, argPlatform := parseValArgs(args)
	req.Region, req.Platform = argRegion, argPlatform
	if req.Region == "" {
		req.Region = cfg.Region
	}
	if req.Platform == "" {
		req.Platform = cfg.Platform
	}

	switch {
	case cmd.noBroadcasterFallback:
		req.Account = argAccount
		if req.Account == "" {
			req.Account = cfg.Account
		}
	default:
		req.Account = resolveAccount(accountSources{Arg: argAccount, Linked: cfg.Account, BroadcasterLogin: c.Env.BroadcasterUserLogin})
	}
	return req
}

// valSubject names what a failure chats about: the rotation has no target
// player, so its errors name the feature instead of an account.
func valSubject(cmd valCommand, req gossiprpc.Request) string {
	if cmd.accountless {
		return "daily rotation"
	}
	return req.Account
}

// valSpecial applies a command's empty-state override, if one is wired.
func valSpecial(special func(any) (string, bool), reply any) (string, bool) {
	if special == nil {
		return "", false
	}
	return special(reply)
}

// valDispatchRun routes !val's first argument word onto the subcommand
// runners: "matches"/"account"/"lb"/"shop" select one explicitly, and anything
// else — nothing, or a Riot ID — is a rank lookup, so "!val Frosty#EUW1" reads
// naturally.
func valDispatchRun(runs valRuns) module.RunFunc {
	return func(ctx context.Context, c *module.Context, args string, emit module.Emit) error {
		sub, rest, _ := strings.Cut(strings.TrimSpace(args), " ")
		switch strings.ToLower(sub) {
		case "match", "matches", "history":
			return runs.matches(ctx, c, rest, emit)
		case "account", "who":
			return runs.account(ctx, c, rest, emit)
		case "lb", "leaderboard", "top":
			return runs.board(ctx, c, rest, emit)
		case "shop", "rotation":
			return runs.shop(ctx, c, rest, emit)
		case "rank", "standing":
			return runs.rank(ctx, c, rest, emit)
		default:
			return runs.rank(ctx, c, args, emit)
		}
	}
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
func valRankTokens() map[string]func(any) string {
	type reply = gossiprpc.ValorantRankReply
	return map[string]func(any) string{
		"player":     func(v any) string { return v.(*reply).Player },
		"region":     func(v any) string { return v.(*reply).Region },
		"tier":       func(v any) string { return v.(*reply).Tier },
		"elo":        func(v any) string { return i64(int64(v.(*reply).Elo)) },
		"rr":         func(v any) string { return i64(int64(v.(*reply).RR)) },
		"lastchange": func(v any) string { return signed(v.(*reply).LastChange) },
		"peaktier":   func(v any) string { return v.(*reply).PeakTier },
		"placement":  func(v any) string { return i64(int64(v.(*reply).Placement)) },
	}
}

// valMatchTokens is the !valmatches palette: the games joined as one-liners
// (bounded by the upstream at five, so no truncation budget is needed) plus
// the age of the most recent one.
func valMatchTokens() map[string]func(any) string {
	type reply = gossiprpc.ValorantMatchesReply
	return map[string]func(any) string{
		"player": func(v any) string { return v.(*reply).Player },
		"region": func(v any) string { return v.(*reply).Region },
		"count":  func(v any) string { return i64(int64(len(v.(*reply).Matches))) },
		"matches": func(v any) string {
			r := v.(*reply)
			parts := make([]string, len(r.Matches))
			for i, m := range r.Matches {
				parts[i] = fmt.Sprintf("%s %d/%d/%d %s on %s", m.Agent, m.Kills, m.Deaths, m.Assists, m.Result, m.Map)
			}
			return strings.Join(parts, ", ")
		},
		"lastago": func(v any) string {
			if m := v.(*reply).Matches; len(m) > 0 {
				return valAgo(m[0].AgoSeconds)
			}
			return ""
		},
	}
}

// valAccountTokens is the !valaccount palette.
func valAccountTokens() map[string]func(any) string {
	type reply = gossiprpc.ValorantAccountReply
	return map[string]func(any) string{
		"player": func(v any) string { return v.(*reply).Player },
		"puuid":  func(v any) string { return v.(*reply).Puuid },
		"region": func(v any) string { return v.(*reply).Region },
		"level":  func(v any) string { return i64(int64(v.(*reply).AccountLevel)) },
		"card":   func(v any) string { return v.(*reply).Card },
		"title":  func(v any) string { return v.(*reply).Title },
	}
}

// valBoardTokens is the !vallb palette: the rows joined as "#rank player (RR)"
// (bounded by the upstream at ten, so no truncation budget is needed).
func valBoardTokens() map[string]func(any) string {
	type reply = gossiprpc.ValorantLeaderboardReply
	return map[string]func(any) string{
		"player": func(v any) string { return v.(*reply).Player },
		"board":  func(v any) string { return v.(*reply).Board },
		"count":  func(v any) string { return i64(int64(len(v.(*reply).Entries))) },
		"entries": func(v any) string {
			entries := v.(*reply).Entries
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
func valShopTokens() map[string]func(any) string {
	type reply = gossiprpc.ValorantShopReply
	return map[string]func(any) string{
		"count": func(v any) string { return i64(int64(v.(*reply).Count)) },
		"items": func(v any) string {
			items := v.(*reply).Items
			parts := make([]string, len(items))
			for i, item := range items {
				parts[i] = item.Name + " (" + i64(item.Price) + " VP)"
			}
			return strings.Join(parts, ", ")
		},
		"reset": func(v any) string { return valResetIn(v.(*reply).ResetUnix) },
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
