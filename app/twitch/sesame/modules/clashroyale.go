// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package modules

import (
	"strings"
	"time"

	"ItsBagelBot/app/twitch/sesame/engine"
	"ItsBagelBot/app/twitch/sesame/module"
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
	// linkedAccountConfig carries account/accountUuid/linkedOnly. Clash Royale
	// has no uuid of its own (a player tag is the identity), so accountUuid
	// stays unset here and is never consulted: the resolution asks for it only
	// under PreferUUID, which this module does not set.
	linkedAccountConfig

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
	statsRun := externalCommand[clashroyaleConfig, gossiprpc.ClashRoyaleStatsReply]{
		route:    clashRoute("stats"),
		enabled:  func(c clashroyaleConfig) string { return c.StatsEnabled },
		message:  func(c clashroyaleConfig) string { return c.StatsMessage },
		fallback: defaultClashStatsTemplate,
		tokens:   clashStatsTokens(),
		// Clash Royale is tag-keyed: never substitute a stored uuid.
		preferName: true,
	}.run(d)
	decksRun := externalCommand[clashroyaleConfig, gossiprpc.ClashRoyaleDecksReply]{
		route:      clashRoute("decks"),
		enabled:    func(c clashroyaleConfig) string { return c.DecksEnabled },
		message:    func(c clashroyaleConfig) string { return c.DecksMessage },
		fallback:   defaultClashDecksTemplate,
		tokens:     clashDecksTokens(),
		preferName: true,
	}.run(d)
	rankedRun := externalCommand[clashroyaleConfig, gossiprpc.ClashRoyaleRankedReply]{
		route:      clashRoute("ranked"),
		enabled:    func(c clashroyaleConfig) string { return c.RankedEnabled },
		message:    func(c clashroyaleConfig) string { return c.RankedMessage },
		fallback:   defaultClashRankedTemplate,
		tokens:     clashRankedTokens(),
		preferName: true,
		special:    clashRankedSpecial,
	}.run(d)
	roadRun := externalCommand[clashroyaleConfig, gossiprpc.ClashRoyaleTrophyRoadReply]{
		route:      clashRoute("trophy_road"),
		enabled:    func(c clashroyaleConfig) string { return c.RoadEnabled },
		message:    func(c clashroyaleConfig) string { return c.RoadMessage },
		fallback:   defaultClashRoadTemplate,
		tokens:     clashRoadTokens(),
		preferName: true,
	}.run(d)

	m := module.NewModule(clashroyaleModuleName, module.KindOptIn)
	m.Command("cr").Everyone().Cooldown(clashroyaleCooldown).
		Run(subDispatch(statsRun, map[string]module.RunFunc{
			"decks": decksRun, "deck": decksRun,
			"ranked": rankedRun, "pol": rankedRun,
			"road": roadRun, "trophy": roadRun, "trophies": roadRun,
		}))
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

// clashRoute names one Clash Royale endpoint. All four ride gossip's one
// shared profile cache, so a viewer reading several views of the same player
// spends a single upstream request per 5 minutes.
func clashRoute(endpoint string) engine.GossipRoute {
	return engine.GossipRoute{Provider: "clashroyale", Endpoint: endpoint}
}

// clashRankedSpecial answers !crranked when the player has no Path of Legends
// record: every numeric token would render zero, so the line says why instead.
func clashRankedSpecial(_ statsCall[clashroyaleConfig], r *gossiprpc.ClashRoyaleRankedReply) (string, bool) {
	if !r.Unranked {
		return "", false
	}
	return unrankedText(r.Player), true
}

// clashStatsTokens is the !crstats template palette over the gossip reply.
func clashStatsTokens() module.TokenExpander[gossiprpc.ClashRoyaleStatsReply] {
	type reply = gossiprpc.ClashRoyaleStatsReply
	return module.TokenExpander[reply]{
		"player":         func(r *reply) string { return r.Player },
		"tag":            func(r *reply) string { return r.Tag },
		"level":          func(r *reply) string { return i64(int64(r.KingLevel)) },
		"wins":           func(r *reply) string { return i64(int64(r.Wins)) },
		"losses":         func(r *reply) string { return i64(int64(r.Losses)) },
		"draws":          func(r *reply) string { return i64(int64(r.Draws)) },
		"battles":        func(r *reply) string { return i64(int64(r.Battles)) },
		"winrate":        func(r *reply) string { return trimScore(r.WinRate) },
		"crowns":         func(r *reply) string { return i64(int64(r.ThreeCrownWins)) },
		"challengemax":   func(r *reply) string { return i64(int64(r.ChallengeMaxWins)) },
		"donations":      func(r *reply) string { return i64(int64(r.Donations)) },
		"totaldonations": func(r *reply) string { return i64(int64(r.TotalDonations)) },
		"clan": func(r *reply) string {
			if clan := r.Clan.Name; clan != "" {
				return clan
			}
			return "no clan"
		},
		"favcard": func(r *reply) string { return r.FavouriteCard.Name },
	}
}

// clashDecksTokens is the !crdecks palette: the deck joined as names (bounded
// by the game at 8 cards plus one tower troop, so no truncation budget is
// needed), the elixir average gossip precomputed, and the count.
func clashDecksTokens() module.TokenExpander[gossiprpc.ClashRoyaleDecksReply] {
	type reply = gossiprpc.ClashRoyaleDecksReply
	names := func(cards []gossiprpc.ClashRoyaleCard) string {
		parts := make([]string, len(cards))
		for i, card := range cards {
			parts[i] = card.Name
		}
		return strings.Join(parts, ", ")
	}
	return module.TokenExpander[reply]{
		"player":  func(r *reply) string { return r.Player },
		"tag":     func(r *reply) string { return r.Tag },
		"cards":   func(r *reply) string { return names(r.CurrentDeck) },
		"support": func(r *reply) string { return names(r.SupportCards) },
		"elixir":  func(r *reply) string { return trimScore(r.AverageElixir) },
		"count":   func(r *reply) string { return i64(int64(len(r.CurrentDeck))) },
	}
}

// clashRankedTokens is the !crranked palette over the PoL result (gossip
// already fell back to legacy league seasons where they are the only record).
func clashRankedTokens() module.TokenExpander[gossiprpc.ClashRoyaleRankedReply] {
	type reply = gossiprpc.ClashRoyaleRankedReply
	return module.TokenExpander[reply]{
		"player":       func(r *reply) string { return r.Player },
		"tag":          func(r *reply) string { return r.Tag },
		"league":       func(r *reply) string { return i64(int64(r.Current.LeagueNumber)) },
		"trophies":     func(r *reply) string { return i64(int64(r.Current.Trophies)) },
		"rank":         func(r *reply) string { return i64(int64(r.Current.Rank)) },
		"prevleague":   func(r *reply) string { return i64(int64(r.Previous.LeagueNumber)) },
		"prevtrophies": func(r *reply) string { return i64(int64(r.Previous.Trophies)) },
		"bestleague":   func(r *reply) string { return i64(int64(r.Best.LeagueNumber)) },
		"besttrophies": func(r *reply) string { return i64(int64(r.Best.Trophies)) },
		"bestrank":     func(r *reply) string { return i64(int64(r.Best.Rank)) },
	}
}

// clashRoadTokens is the !crroad palette.
func clashRoadTokens() module.TokenExpander[gossiprpc.ClashRoyaleTrophyRoadReply] {
	type reply = gossiprpc.ClashRoyaleTrophyRoadReply
	return module.TokenExpander[reply]{
		"player":       func(r *reply) string { return r.Player },
		"tag":          func(r *reply) string { return r.Tag },
		"trophies":     func(r *reply) string { return i64(int64(r.Trophies)) },
		"besttrophies": func(r *reply) string { return i64(int64(r.BestTrophies)) },
		"arena":        func(r *reply) string { return r.Arena.Name },
	}
}

// unrankedText renders !crranked's answer when the player has no PoL record.
func unrankedText(player string) string {
	return player + " " + clashroyaleUnrankedText
}
