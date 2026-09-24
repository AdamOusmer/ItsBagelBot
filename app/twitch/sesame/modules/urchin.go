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
	defaultUrchinDailyTemplate          = "{player} today: {wins}W {losses}L · {finals} finals · {beds} beds · {fkdr} FKDR"
	defaultUrchinWeeklyTemplate         = "{player} this week: {wins}W {losses}L · {finals} finals · {beds} beds · {fkdr} FKDR"
	defaultUrchinMonthlyTemplate        = "{player} this month: {wins}W {losses}L · {finals} finals · {beds} beds · {fkdr} FKDR"
	defaultUrchinStatsTemplate          = "{player}: {stars} stars · {wins} wins · {finals} finals · {fkdr} FKDR · {beds} beds broken"
	defaultUrchinSniperTemplate         = "{player} urchin score: {score}"
	defaultUrchinTagsTemplate           = "{player}: {tags}"
	defaultUrchinTagDescriptionTemplate = "{player}: {tags}"
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
		Run(urchinStatsRun(d))
	m.Command("sniper").Everyone().Cooldown(urchinCooldown).Aliases("urchin").
		Run(urchinSniperRun(d))
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
		tokens: module.TokenExpander[reply]{
			"player":      func(r *reply) string { return r.Player },
			"wins":        func(r *reply) string { return i64(r.Wins) },
			"losses":      func(r *reply) string { return i64(r.Losses) },
			"finals":      func(r *reply) string { return i64(r.FinalKills) },
			"finaldeaths": func(r *reply) string { return i64(r.FinalDeaths) },
			"beds":        func(r *reply) string { return i64(r.BedsBroken) },
			"games":       func(r *reply) string { return i64(r.GamesPlayed) },
			"levels":      func(r *reply) string { return i64(r.Levels) },
			"fkdr":        func(r *reply) string { return ratio(r.FinalKills, r.FinalDeaths) },
		},
	}.run(d)
}

func urchinStatsRun(d engine.Deps) module.RunFunc {
	type reply = gossiprpc.HypixelStatsReply
	return externalCommand[urchinConfig, reply]{
		route:    urchinRoute("hypixel", "stats"),
		enabled:  func(c urchinConfig) string { return c.StatsEnabled },
		message:  func(c urchinConfig) string { return c.StatsMessage },
		fallback: defaultUrchinStatsTemplate,
		tokens: module.TokenExpander[reply]{
			"player":      func(r *reply) string { return r.Player },
			"stars":       func(r *reply) string { return i64(r.Stars) },
			"wins":        func(r *reply) string { return i64(r.Wins) },
			"losses":      func(r *reply) string { return i64(r.Losses) },
			"finals":      func(r *reply) string { return i64(r.FinalKills) },
			"finaldeaths": func(r *reply) string { return i64(r.FinalDeaths) },
			"beds":        func(r *reply) string { return i64(r.BedsBroken) },
			"fkdr":        func(r *reply) string { return ratio(r.FinalKills, r.FinalDeaths) },
			"wlr":         func(r *reply) string { return ratio(r.Wins, r.Losses) },
		},
	}.run(d)
}

func urchinSniperRun(d engine.Deps) module.RunFunc {
	type reply = gossiprpc.UrchinSniperReply
	return externalCommand[urchinConfig, reply]{
		route:    urchinRoute("urchin", "sniper"),
		enabled:  func(c urchinConfig) string { return c.SniperEnabled },
		message:  func(c urchinConfig) string { return c.SniperMessage },
		fallback: defaultUrchinSniperTemplate,
		tokens: module.TokenExpander[reply]{
			"player":   func(r *reply) string { return r.Player },
			"score":    func(r *reply) string { return trimScore(r.Score) },
			"mode":     func(r *reply) string { return r.Mode },
			"tagcount": func(r *reply) string { return i64(int64(r.TagCount)) },
		},
	}.run(d)
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
