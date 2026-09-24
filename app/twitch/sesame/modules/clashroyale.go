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

const clashroyaleModuleName = "clashroyale"

const clashroyaleCooldown = 10 * time.Second

const (
	defaultClashStatsTemplate  = "{player} · level {level} · {wins}W/{losses}L · {winrate}% WR · {crowns} three-crowns · {clan}"
	defaultClashDecksTemplate  = "{player}'s deck ({count}/8): {cards} · avg elixir {elixir}"
	defaultClashRankedTemplate = "{player} Path of Legends: league {league} · {trophies} trophies · rank #{rank} · best {besttrophies}"
	defaultClashRoadTemplate   = "{player}: {trophies} trophies · best {besttrophies} · {arena}"
)

const clashroyaleUnrankedText = "has no Path of Legends record this season"

type clashroyaleConfig struct {
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

func ClashRoyale(d engine.Deps) module.Module {
	statsRun := externalCommand[clashroyaleConfig, gossiprpc.ClashRoyaleStatsReply]{
		route:      clashRoute("stats"),
		enabled:    func(c clashroyaleConfig) string { return c.StatsEnabled },
		message:    func(c clashroyaleConfig) string { return c.StatsMessage },
		fallback:   defaultClashStatsTemplate,
		tokens:     clashStatsTokens(),
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

func clashRoute(endpoint string) engine.GossipRoute {
	return engine.GossipRoute{Provider: "clashroyale", Endpoint: endpoint}
}

func clashRankedSpecial(_ statsCall[clashroyaleConfig], r *gossiprpc.ClashRoyaleRankedReply) (string, bool) {
	if !r.Unranked {
		return "", false
	}
	return unrankedText(r.Player), true
}

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

func unrankedText(player string) string {
	return player + " " + clashroyaleUnrankedText
}
