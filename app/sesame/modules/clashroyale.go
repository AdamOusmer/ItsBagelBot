// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package modules

import (
	"context"
	"strings"
	"time"

	"ItsBagelBot/app/sesame/engine"
	"ItsBagelBot/app/sesame/module"
	"ItsBagelBot/internal/domain/outgress"
	gossiprpc "ItsBagelBot/internal/domain/rpc/gossip"
)

// clashroyaleModuleName is the ModuleView key; the console MODULE_CATALOG entry
// and the dashboard module page use the same id.
const clashroyaleModuleName = "clashroyale"

// clashroyaleCooldown is the shared per-command window; gossip caches the one
// shared player profile all four commands project, so this only shields chat
// from command spam, not the API.
const clashroyaleCooldown = 10 * time.Second

// Default reply templates. The broadcaster customizes them per command on the
// module page; blank falls back to these.
const (
	defaultClashStatsTemplate  = "{player} · level {level} · {wins}W/{losses}L · {winrate}% WR · {crowns} three-crowns · {clan}"
	defaultClashDecksTemplate  = "{player}'s deck ({count}/8): {cards} · avg elixir {elixir}"
	defaultClashRankedTemplate = "{player} Path of Legends: league {league} · {trophies} trophies · rank #{rank} · best {besttrophies}"
	defaultClashRoadTemplate   = "{player}: {trophies} trophies · best {besttrophies} · {arena}"
)

// clashroyaleUnrankedText replaces !crranked's template when gossip answers
// Unranked: every numeric token would render zero, so the default line says
// why instead (the same shape as !fn session's no-snapshot case).
const clashroyaleUnrankedText = "has no Path of Legends record this season"

// clashroyaleConfig is the module's dashboard configuration. Account is the
// linked player tag (blank = the broadcaster's own Twitch login, which the
// provider almost always rejects — Clash Royale has no name lookup, so a tag
// is required). The *Enabled toggles are stored "on"/"off" — empty means on,
// matching the alerts module's semantics — and each *Message is a customized
// template (blank = default).
type clashroyaleConfig struct {
	Account string `json:"account"`

	StatsEnabled  string `json:"statsEnabled"`
	StatsMessage  string `json:"statsMessage"`
	DecksEnabled  string `json:"decksEnabled"`
	DecksMessage  string `json:"decksMessage"`
	RankedEnabled string `json:"rankedEnabled"`
	RankedMessage string `json:"rankedMessage"`
	RoadEnabled   string `json:"roadEnabled"`
	RoadMessage   string `json:"roadMessage"`
}

// ClashRoyale owns the Clash Royale chat commands backed by the gossip
// service. It is a named, opt-in module (KindOptIn): off by default, enabled
// on the dashboard, where the broadcaster links their player tag. Viewers can
// always target another player explicitly: "!cr #P2LQ0GR".
//
// The command surface mirrors fortnite's: one root with subcommands plus the
// squashed forms as direct triggers.
//
//	!cr [tag]           lifetime profile (also !crstats)
//	!cr decks [tag]     current battle deck (also !crdecks)
//	!cr ranked [tag]    Path of Legends standing (also !crranked)
//	!cr road [tag]      trophy-road standing (also !crroad)
//
// All four ride gossip's one shared profile cache, so a viewer reading several
// views of the same player spends a single upstream request per 5 minutes.
func ClashRoyale(d engine.Deps) module.Module {
	rankedSpecial := func(reply any) (string, bool) {
		r, ok := reply.(*gossiprpc.ClashRoyaleRankedReply)
		if !ok || !r.Unranked {
			return "", false
		}
		return unrankedText(r.Player), true
	}
	statsRun := clashRun(d, clashCommand{
		endpoint: "stats",
		enabled:  func(c clashroyaleConfig) string { return c.StatsEnabled },
		message:  func(c clashroyaleConfig) string { return c.StatsMessage },
		fallback: defaultClashStatsTemplate,
		newReply: func() any { return &gossiprpc.ClashRoyaleStatsReply{} },
		tokens:   clashStatsTokens(),
	})
	decksRun := clashRun(d, clashCommand{
		endpoint: "decks",
		enabled:  func(c clashroyaleConfig) string { return c.DecksEnabled },
		message:  func(c clashroyaleConfig) string { return c.DecksMessage },
		fallback: defaultClashDecksTemplate,
		newReply: func() any { return &gossiprpc.ClashRoyaleDecksReply{} },
		tokens:   clashDecksTokens(),
	})
	rankedRun := clashRun(d, clashCommand{
		endpoint: "ranked",
		enabled:  func(c clashroyaleConfig) string { return c.RankedEnabled },
		message:  func(c clashroyaleConfig) string { return c.RankedMessage },
		fallback: defaultClashRankedTemplate,
		newReply: func() any { return &gossiprpc.ClashRoyaleRankedReply{} },
		tokens:   clashRankedTokens(),
		special:  rankedSpecial,
	})
	roadRun := clashRun(d, clashCommand{
		endpoint: "trophy_road",
		enabled:  func(c clashroyaleConfig) string { return c.RoadEnabled },
		message:  func(c clashroyaleConfig) string { return c.RoadMessage },
		fallback: defaultClashRoadTemplate,
		newReply: func() any { return &gossiprpc.ClashRoyaleTrophyRoadReply{} },
		tokens:   clashRoadTokens(),
	})

	m := module.NewModule(clashroyaleModuleName, module.KindOptIn)
	m.Command("cr").Everyone().Cooldown(clashroyaleCooldown).
		Run(clashDispatchRun(statsRun, decksRun, rankedRun, roadRun))
	m.Command("crstats").Everyone().Cooldown(clashroyaleCooldown).Aliases("clashroyale").
		Run(statsRun)
	m.Command("crdecks").Everyone().Cooldown(clashroyaleCooldown).Aliases("crdeck").
		Run(decksRun)
	m.Command("crranked").Everyone().Cooldown(clashroyaleCooldown).Aliases("crpol").
		Run(rankedRun)
	m.Command("crroad").Everyone().Cooldown(clashroyaleCooldown).Aliases("crtrophy").
		Run(roadRun)
	return m.Build()
}

// clashCommand names one command's wiring: the gossip endpoint it queries,
// where its toggle and template live in the config blob, an empty reply value
// Call decodes into, and an optional post-decode override (ranked's
// no-record answer replaces its template entirely).
type clashCommand struct {
	endpoint string
	enabled  func(clashroyaleConfig) string
	message  func(clashroyaleConfig) string
	fallback string
	newReply func() any
	tokens   map[string]func(any) string
	special  func(reply any) (string, bool)
}

// clashRun builds one command runner: resolve the target tag, call the
// endpoint, expand the template over the reply. Tokens are declared against
// `any` so one runner serves four reply types; each palette casts back.
func clashRun(d engine.Deps, cmd clashCommand) module.RunFunc {
	return func(ctx context.Context, c *module.Context, args string, emit module.Emit) error {
		var cfg clashroyaleConfig
		_ = c.Decode(&cfg)
		if !alertOn(cmd.enabled(cfg)) || d.Gossip == nil {
			return nil
		}

		tag := resolveAccount(accountSources{Arg: args, Linked: cfg.Account, BroadcasterLogin: c.Env.BroadcasterUserLogin})
		req := gossiprpc.Request{Account: tag, IsPremium: c.Regress.IsPremium()}
		reply := cmd.newReply()
		route := engine.GossipRoute{Provider: "clashroyale", Endpoint: cmd.endpoint}
		if err := d.Gossip.Call(ctx, route, req, reply); err != nil {
			return clashCallErr(c, emit, tag, err)
		}
		emit(&module.Output{Type: outgress.TypeChat, BroadcasterID: c.Env.BroadcasterUserID, Text: clashRender(cmd, cfg, reply)})
		return nil
	}
}

// clashCallErr maps a gossip failure onto the command's outcome:
// reply-level errors (player not found, ...) were answered in chat and count
// as handled; infrastructure errors also chat a retry hint but propagate so
// the caller still logs them.
func clashCallErr(c *module.Context, emit module.Emit, tag string, err error) error {
	if chatReplyError(c, emit, tag, err) {
		return nil
	}
	return err
}

// clashRender picks the final chat line: a command's special-case override
// when it fires (ranked's no-record answer), otherwise the configured
// template expanded over the gossip reply.
func clashRender(cmd clashCommand, cfg clashroyaleConfig, reply any) string {
	if cmd.special != nil {
		if text, ok := cmd.special(reply); ok {
			return text
		}
	}
	return module.ExpandString(orDefault(cmd.message(cfg), cmd.fallback), func(key string) (string, bool) {
		if field, ok := cmd.tokens[key]; ok {
			return field(reply), true
		}
		return module.ParseDynamic(key)
	})
}

// clashDispatchRun routes !cr's first argument word onto the subcommand
// runners: "decks"/"ranked"/"road" select one explicitly, and anything else —
// nothing, or a player tag — is a profile lookup, so "!cr #P2LQ0GR" reads
// naturally.
func clashDispatchRun(statsRun, decksRun, rankedRun, roadRun module.RunFunc) module.RunFunc {
	return func(ctx context.Context, c *module.Context, args string, emit module.Emit) error {
		sub, rest, _ := strings.Cut(strings.TrimSpace(args), " ")
		switch strings.ToLower(sub) {
		case "decks", "deck":
			return decksRun(ctx, c, rest, emit)
		case "ranked", "pol":
			return rankedRun(ctx, c, rest, emit)
		case "road", "trophy", "trophies":
			return roadRun(ctx, c, rest, emit)
		default:
			return statsRun(ctx, c, args, emit)
		}
	}
}

// clashStatsTokens is the !crstats template palette over the gossip reply.
func clashStatsTokens() map[string]func(any) string {
	type reply = gossiprpc.ClashRoyaleStatsReply
	return map[string]func(any) string{
		"player":         func(v any) string { return v.(*reply).Player },
		"tag":            func(v any) string { return v.(*reply).Tag },
		"level":          func(v any) string { return i64(int64(v.(*reply).KingLevel)) },
		"wins":           func(v any) string { return i64(int64(v.(*reply).Wins)) },
		"losses":         func(v any) string { return i64(int64(v.(*reply).Losses)) },
		"draws":          func(v any) string { return i64(int64(v.(*reply).Draws)) },
		"battles":        func(v any) string { return i64(int64(v.(*reply).Battles)) },
		"winrate":        func(v any) string { return trimScore(v.(*reply).WinRate) },
		"crowns":         func(v any) string { return i64(int64(v.(*reply).ThreeCrownWins)) },
		"challengemax":   func(v any) string { return i64(int64(v.(*reply).ChallengeMaxWins)) },
		"donations":      func(v any) string { return i64(int64(v.(*reply).Donations)) },
		"totaldonations": func(v any) string { return i64(int64(v.(*reply).TotalDonations)) },
		"clan": func(v any) string {
			if clan := v.(*reply).Clan.Name; clan != "" {
				return clan
			}
			return "no clan"
		},
		"favcard": func(v any) string { return v.(*reply).FavouriteCard.Name },
	}
}

// clashDecksTokens is the !crdecks palette: the deck joined as names (bounded
// by the game at 8 cards plus one tower troop, so no truncation budget is
// needed), the elixir average gossip precomputed, and the count.
func clashDecksTokens() map[string]func(any) string {
	type reply = gossiprpc.ClashRoyaleDecksReply
	names := func(cards []gossiprpc.ClashRoyaleCard) string {
		parts := make([]string, len(cards))
		for i, card := range cards {
			parts[i] = card.Name
		}
		return strings.Join(parts, ", ")
	}
	return map[string]func(any) string{
		"player":  func(v any) string { return v.(*reply).Player },
		"tag":     func(v any) string { return v.(*reply).Tag },
		"cards":   func(v any) string { return names(v.(*reply).CurrentDeck) },
		"support": func(v any) string { return names(v.(*reply).SupportCards) },
		"elixir":  func(v any) string { return trimScore(v.(*reply).AverageElixir) },
		"count":   func(v any) string { return i64(int64(len(v.(*reply).CurrentDeck))) },
	}
}

// clashRankedTokens is the !crranked palette over the PoL result (gossip
// already fell back to legacy league seasons where they are the only record).
func clashRankedTokens() map[string]func(any) string {
	type reply = gossiprpc.ClashRoyaleRankedReply
	return map[string]func(any) string{
		"player":       func(v any) string { return v.(*reply).Player },
		"tag":          func(v any) string { return v.(*reply).Tag },
		"league":       func(v any) string { return i64(int64(v.(*reply).Current.LeagueNumber)) },
		"trophies":     func(v any) string { return i64(int64(v.(*reply).Current.Trophies)) },
		"rank":         func(v any) string { return i64(int64(v.(*reply).Current.Rank)) },
		"prevleague":   func(v any) string { return i64(int64(v.(*reply).Previous.LeagueNumber)) },
		"prevtrophies": func(v any) string { return i64(int64(v.(*reply).Previous.Trophies)) },
		"bestleague":   func(v any) string { return i64(int64(v.(*reply).Best.LeagueNumber)) },
		"besttrophies": func(v any) string { return i64(int64(v.(*reply).Best.Trophies)) },
		"bestrank":     func(v any) string { return i64(int64(v.(*reply).Best.Rank)) },
	}
}

// clashRoadTokens is the !crroad palette.
func clashRoadTokens() map[string]func(any) string {
	type reply = gossiprpc.ClashRoyaleTrophyRoadReply
	return map[string]func(any) string{
		"player":       func(v any) string { return v.(*reply).Player },
		"tag":          func(v any) string { return v.(*reply).Tag },
		"trophies":     func(v any) string { return i64(int64(v.(*reply).Trophies)) },
		"besttrophies": func(v any) string { return i64(int64(v.(*reply).BestTrophies)) },
		"arena":        func(v any) string { return v.(*reply).Arena.Name },
	}
}

// unrankedText renders !crranked's answer when the player has no PoL record.
func unrankedText(player string) string {
	return player + " " + clashroyaleUnrankedText
}
