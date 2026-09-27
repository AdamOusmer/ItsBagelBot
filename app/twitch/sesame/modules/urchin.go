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

	"go.uber.org/zap"
)

const urchinModuleName = "urchin"

const urchinCooldown = 5 * time.Second

const (
	defaultUrchinDailyTemplate          = "{urchin:player} today: {urchin:wins}W {urchin:losses}L · {urchin:finals} finals · {urchin:beds} beds · {urchin:fkdr} FKDR"
	defaultUrchinWeeklyTemplate         = "{urchin:player} this week: {urchin:wins}W {urchin:losses}L · {urchin:finals} finals · {urchin:beds} beds · {urchin:fkdr} FKDR"
	defaultUrchinMonthlyTemplate        = "{urchin:player} this month: {urchin:wins}W {urchin:losses}L · {urchin:finals} finals · {urchin:beds} beds · {urchin:fkdr} FKDR"
	defaultUrchinStatsTemplate          = "{urchin:player}: {urchin:stars} stars · {urchin:wins} wins · {urchin:finals} finals · {urchin:fkdr} FKDR · {urchin:beds} beds broken"
	defaultUrchinSniperTemplate         = "{urchin:player} urchin score: {urchin:score}"
	defaultUrchinTagsTemplate           = "{urchin:player}: {urchin:tags}"
	defaultUrchinTagDescriptionTemplate = "{urchin:player}: {urchin:tags}"
)

type urchinConfig struct {
	linkedAccountConfig

	DailyEnabled          string `json:"dailyEnabled"`
	DailyMessage          string `json:"dailyMessage"`
	WeeklyEnabled         string `json:"weeklyEnabled"`
	WeeklyMessage         string `json:"weeklyMessage"`
	MonthlyEnabled        string `json:"monthlyEnabled"`
	MonthlyMessage        string `json:"monthlyMessage"`
	StatsEnabled          string `json:"statsEnabled"`
	StatsMessage          string `json:"statsMessage"`
	SniperEnabled         string `json:"sniperEnabled"`
	SniperMessage         string `json:"sniperMessage"`
	TagsEnabled           string `json:"tagsEnabled"`
	TagsMessage           string `json:"tagsMessage"`
	TagDescriptionEnabled string `json:"tagDescriptionEnabled"`
	TagDescriptionMessage string `json:"tagDescriptionMessage"`
}

func Urchin(d engine.Deps) module.Module {
	log := d.Log
	if log == nil {
		log = zap.NewNop()
	}

	m := module.NewModule(urchinModuleName, module.KindOptIn)
	m.Command("daily").Everyone().Cooldown(urchinCooldown).Aliases("bwdaily").
		Run(urchinSessionRun(d, urchinDailyWindow))
	m.Command("weekly").Everyone().Cooldown(urchinCooldown).Aliases("bwweekly").
		Run(urchinSessionRun(d, urchinWeeklyWindow))
	m.Command("monthly").Everyone().Cooldown(urchinCooldown).Aliases("bwmonthly").
		Run(urchinSessionRun(d, urchinMonthlyWindow))
	m.Command("bwstats").Everyone().Cooldown(urchinCooldown).Aliases("bedwars").
		Run(urchinStatsCommand.run(d))
	m.Command("sniper").Everyone().Cooldown(urchinCooldown).Aliases("urchin").
		Run(urchinSniperCommand.run(d))
	m.Command("tag").Everyone().Cooldown(urchinCooldown).Aliases("tags", "bwtags").
		Run(urchinTagsRun(d))
	m.Command("tagdescription").Everyone().Cooldown(urchinCooldown).
		Run(urchinTagDescriptionRun(d))
	return m.Build()
}

func urchinRoute(provider, endpoint string) engine.GossipRoute {
	return engine.GossipRoute{Provider: provider, Endpoint: endpoint}
}

type urchinWindow struct {
	endpoint string
	enabled  func(urchinConfig) string
	message  func(urchinConfig) string
	fallback string
}

var (
	urchinDailyWindow = urchinWindow{
		endpoint: "daily",
		enabled:  func(c urchinConfig) string { return c.DailyEnabled },
		message:  func(c urchinConfig) string { return c.DailyMessage },
		fallback: defaultUrchinDailyTemplate,
	}
	urchinWeeklyWindow = urchinWindow{
		endpoint: "weekly",
		enabled:  func(c urchinConfig) string { return c.WeeklyEnabled },
		message:  func(c urchinConfig) string { return c.WeeklyMessage },
		fallback: defaultUrchinWeeklyTemplate,
	}
	urchinMonthlyWindow = urchinWindow{
		endpoint: "monthly",
		enabled:  func(c urchinConfig) string { return c.MonthlyEnabled },
		message:  func(c urchinConfig) string { return c.MonthlyMessage },
		fallback: defaultUrchinMonthlyTemplate,
	}
)

func urchinSessionRun(d engine.Deps, w urchinWindow) module.RunFunc {
	type reply = gossiprpc.UrchinSessionReply
	return externalCommand[urchinConfig, reply]{
		route:    urchinRoute("urchin", w.endpoint),
		enabled:  w.enabled,
		message:  w.message,
		fallback: w.fallback,
		tokens:   urchinSessionTokens(),
	}.run(d)
}

var urchinStatsCommand = externalCommand[urchinConfig, gossiprpc.HypixelStatsReply]{
	route:    urchinRoute("hypixel", "stats"),
	enabled:  func(c urchinConfig) string { return c.StatsEnabled },
	message:  func(c urchinConfig) string { return c.StatsMessage },
	fallback: defaultUrchinStatsTemplate,
	tokens:   urchinStatsTokens(),
}

var urchinSniperCommand = externalCommand[urchinConfig, gossiprpc.UrchinSniperReply]{
	route:    urchinRoute("urchin", "sniper"),
	enabled:  func(c urchinConfig) string { return c.SniperEnabled },
	message:  func(c urchinConfig) string { return c.SniperMessage },
	fallback: defaultUrchinSniperTemplate,
	tokens:   urchinSniperTokens(),
}

func urchinTagsRun(d engine.Deps) module.RunFunc {
	return urchinTagRun(d, urchinTagCommand{
		enabled:  func(c urchinConfig) string { return c.TagsEnabled },
		message:  func(c urchinConfig) string { return c.TagsMessage },
		fallback: defaultUrchinTagsTemplate,
		format:   formatUrchinTags,
	})
}

func urchinTagDescriptionRun(d engine.Deps) module.RunFunc {
	return urchinTagRun(d, urchinTagCommand{
		enabled:  func(c urchinConfig) string { return c.TagDescriptionEnabled },
		message:  func(c urchinConfig) string { return c.TagDescriptionMessage },
		fallback: defaultUrchinTagDescriptionTemplate,
		format:   formatUrchinTagDescriptions,
	})
}

type urchinTagCommand struct {
	enabled  func(urchinConfig) string
	message  func(urchinConfig) string
	fallback string
	format   func([]gossiprpc.UrchinTag) string
}

func urchinTagRun(d engine.Deps, cmd urchinTagCommand) module.RunFunc {
	type reply = gossiprpc.UrchinTagsReply
	return externalCommand[urchinConfig, reply]{
		route:    urchinRoute("urchin", "tags"),
		enabled:  cmd.enabled,
		message:  cmd.message,
		fallback: cmd.fallback,
		tokens: module.TokenExpander[reply]{
			"player":   func(r *reply) string { return r.Player },
			"tags":     func(r *reply) string { return cmd.format(r.Tags) },
			"tagcount": func(r *reply) string { return i64(int64(len(r.Tags))) },
		},
	}.run(d)
}

func displayTagType(tagType string) string {
	switch tagType {
	case "blatant_cheater":
		return "Blatant Cheater"
	case "confirmed_cheater":
		return "Confirmed Cheater"
	case "closet_cheater":
		return "Closet Cheater"
	case "sniper":
		return "Sniper"
	default:
		s := strings.ReplaceAll(tagType, "_", " ")
		if len(s) > 0 {
			return strings.ToUpper(s[:1]) + s[1:]
		}
		return s
	}
}

func formatUrchinTags(tags []gossiprpc.UrchinTag) string {
	if len(tags) == 0 {
		return "No tags"
	}
	parts := make([]string, 0, len(tags))
	for _, t := range tags {
		name := displayTagType(t.Type)
		if t.AddedOn > 0 {
			name += fmt.Sprintf(" (added %s)", time.Unix(t.AddedOn, 0).UTC().Format("Jan 2, 2006"))
		}
		parts = append(parts, name)
	}
	return strings.Join(parts, ", ")
}

func formatUrchinTagDescriptions(tags []gossiprpc.UrchinTag) string {
	if len(tags) == 0 {
		return "No tags"
	}
	parts := make([]string, 0, len(tags))
	for _, t := range tags {
		name := displayTagType(t.Type)
		var extras []string
		if t.Reason != "" {
			extras = append(extras, t.Reason)
		}
		if t.AddedOn > 0 {
			extras = append(extras, "added "+time.Unix(t.AddedOn, 0).UTC().Format("Jan 2, 2006"))
		}
		if len(extras) > 0 {
			parts = append(parts, fmt.Sprintf("%s (%s)", name, strings.Join(extras, " - ")))
		} else {
			parts = append(parts, name)
		}
	}
	return strings.Join(parts, ", ")
}

type urchinPerformance struct {
	player                                  string
	wins, losses, finals, finalDeaths, beds int64
}

func urchinPerformanceTokens[R any](read func(*R) urchinPerformance) module.TokenExpander[R] {
	return module.TokenExpander[R]{
		"player":      func(r *R) string { return read(r).player },
		"wins":        func(r *R) string { return i64(read(r).wins) },
		"losses":      func(r *R) string { return i64(read(r).losses) },
		"finals":      func(r *R) string { return i64(read(r).finals) },
		"finaldeaths": func(r *R) string { return i64(read(r).finalDeaths) },
		"beds":        func(r *R) string { return i64(read(r).beds) },
		"fkdr":        func(r *R) string { facts := read(r); return ratio(facts.finals, facts.finalDeaths) },
	}
}

func urchinSessionTokens() module.TokenExpander[gossiprpc.UrchinSessionReply] {
	type reply = gossiprpc.UrchinSessionReply
	tokens := urchinPerformanceTokens(func(r *reply) urchinPerformance {
		return urchinPerformance{player: r.Player, wins: r.Wins, losses: r.Losses, finals: r.FinalKills, finalDeaths: r.FinalDeaths, beds: r.BedsBroken}
	})
	tokens["games"] = func(r *reply) string { return i64(r.GamesPlayed) }
	tokens["levels"] = func(r *reply) string { return i64(r.Levels) }
	return tokens
}

func urchinStatsTokens() module.TokenExpander[gossiprpc.HypixelStatsReply] {
	type reply = gossiprpc.HypixelStatsReply
	tokens := urchinPerformanceTokens(func(r *reply) urchinPerformance {
		return urchinPerformance{player: r.Player, wins: r.Wins, losses: r.Losses, finals: r.FinalKills, finalDeaths: r.FinalDeaths, beds: r.BedsBroken}
	})
	tokens["stars"] = func(r *reply) string { return i64(r.Stars) }
	tokens["wlr"] = func(r *reply) string { return ratio(r.Wins, r.Losses) }
	return tokens
}

func urchinSniperTokens() module.TokenExpander[gossiprpc.UrchinSniperReply] {
	type reply = gossiprpc.UrchinSniperReply
	return module.TokenExpander[reply]{
		"player":   func(r *reply) string { return r.Player },
		"score":    func(r *reply) string { return trimScore(r.Score) },
		"mode":     func(r *reply) string { return r.Mode },
		"tagcount": func(r *reply) string { return i64(int64(r.TagCount)) },
	}
}
