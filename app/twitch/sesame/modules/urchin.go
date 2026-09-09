// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package modules

import (
	"fmt"
	"maps"
	"strings"
	"time"

	"ItsBagelBot/app/twitch/sesame/engine"
	"ItsBagelBot/app/twitch/sesame/module"
	gossiprpc "ItsBagelBot/internal/domain/rpc/gossip"

	"go.uber.org/zap"
)

// urchinModuleName is the ModuleView key; the console MODULE_CATALOG entry and
// the dashboard module page use the same id.
const urchinModuleName = "urchin"

// urchinCooldown is the shared per-command window; gossip caches upstream
// replies, so this only shields chat from command spam, not the API. The
// upstream budget is gossip's URCHIN_RATE_LIMIT bucket, not this window.
const urchinCooldown = 5 * time.Second

// Default reply templates. The broadcaster customizes them per command on the
// module page; blank falls back to these.
const (
	defaultUrchinDailyTemplate          = "{player} today: {wins}W {losses}L · {finals} finals · {beds} beds · {fkdr} FKDR"
	defaultUrchinWeeklyTemplate         = "{player} this week: {wins}W {losses}L · {finals} finals · {beds} beds · {fkdr} FKDR"
	defaultUrchinMonthlyTemplate        = "{player} this month: {wins}W {losses}L · {finals} finals · {beds} beds · {fkdr} FKDR"
	defaultUrchinStatsTemplate          = "{player}: {stars} stars · {wins} wins · {finals} finals · {fkdr} FKDR · {beds} beds broken"
	defaultUrchinSniperTemplate         = "{player} urchin score: {score}"
	defaultUrchinTagsTemplate           = "{player}: {tags}"
	defaultUrchinTagDescriptionTemplate = "{player}: {tags}"
)

// urchinConfig is the module's dashboard configuration. Account is the linked
// default account (blank = the broadcaster's own Twitch login). AccountUUID is
// the Mojang uuid stored next to it when the resolve succeeds, so Hypixel and
// Coral lookups skip the name hop and survive a rename. Each *Enabled is a
// per-command toggle stored "on"/"off" — empty means on, matching the alerts
// module's semantics — and each *Message is a customized template (blank =
// default).
type urchinConfig struct {
	// linkedAccountConfig carries account/accountUuid/linkedOnly: the linked
	// Minecraft account, the Mojang uuid stored next to it when the resolve
	// succeeds (so Hypixel and Coral lookups skip the name hop and survive a
	// rename), and the "only my linked account" toggle.
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

// Urchin owns the Hypixel Bed Wars stats commands backed by the urchin.gg
// Coral API through the gossip service. It is a named, opt-in module
// (KindOptIn): off by default, enabled on the dashboard, where the broadcaster
// links a default Minecraft account and can toggle or re-template each
// command. Viewers can always target another player explicitly: "!daily
// somePlayer".
//
// Commands: !daily / !weekly / !monthly (Bed Wars session deltas), !bwstats
// (lifetime stats), !sniper (Urchin/Cubelify overlay score), !tags (active
// blacklist tags).
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

// urchinRoute names one gossip provider/endpoint pair. Most commands ride the
// urchin.gg Coral API; !bwstats rides the hypixel provider instead, a separate
// external system with its own key and budget (Coral cannot serve lifetime
// stats on our key). Which provider answers is not a dashboard concern, so
// both stay on the one urchin module page.
func urchinRoute(provider, endpoint string) engine.GossipRoute {
	return engine.GossipRoute{Provider: provider, Endpoint: endpoint}
}

// urchinWindow is one session command's binding: the Coral endpoint it asks
// and where its toggle and template live in the config blob. !daily, !weekly
// and !monthly differ in nothing else, so they come from these three values
// rather than three near-identical constructors.
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

// urchinSessionRun answers !daily / !weekly / !monthly with the period's Bed
// Wars delta. Template tokens: {player} {wins} {losses} {finals} {finaldeaths}
// {beds} {games} {levels} {fkdr}.
func urchinSessionRun(d engine.Deps, w urchinWindow) module.RunFunc {
	return externalCommand[urchinConfig, gossiprpc.UrchinSessionReply]{
		route:    urchinRoute("urchin", w.endpoint),
		enabled:  w.enabled,
		message:  w.message,
		fallback: w.fallback,
		tokens:   urchinSessionTokens(),
	}.run(d)
}

// bedWarsCore is the Bed Wars slice the period delta and the lifetime profile
// hold in common. The two replies are separate wires (one is Coral's, one is
// Hypixel's) and Go cannot read a field off a type parameter, so the shared
// counters travel as this record instead of as two copies of the same palette
// — a second copy is free to drift, e.g. print {fkdr} to a different precision
// than the command beside it.
type bedWarsCore struct {
	player      string
	wins        int64
	losses      int64
	finals      int64
	finalDeaths int64
	beds        int64
}

// bedWarsTokens is the palette both Bed Wars replies share; callers add the
// tokens only their own reply carries.
func bedWarsTokens[R any](core func(*R) bedWarsCore, extra module.TokenExpander[R]) module.TokenExpander[R] {
	t := module.TokenExpander[R]{
		"player":      func(r *R) string { return core(r).player },
		"wins":        func(r *R) string { return i64(core(r).wins) },
		"losses":      func(r *R) string { return i64(core(r).losses) },
		"finals":      func(r *R) string { return i64(core(r).finals) },
		"finaldeaths": func(r *R) string { return i64(core(r).finalDeaths) },
		"beds":        func(r *R) string { return i64(core(r).beds) },
		"fkdr":        func(r *R) string { c := core(r); return ratio(c.finals, c.finalDeaths) },
	}
	maps.Copy(t, extra)
	return t
}

// sessionBedWars and statsBedWars read the shared counters off each wire. Go
// cannot reach a field through a type parameter, so the two replies hand them
// over as a record rather than the palette being written twice.
func sessionBedWars(r *gossiprpc.UrchinSessionReply) bedWarsCore {
	return bedWarsCore{r.Player, r.Wins, r.Losses, r.FinalKills, r.FinalDeaths, r.BedsBroken}
}

func statsBedWars(r *gossiprpc.HypixelStatsReply) bedWarsCore {
	return bedWarsCore{r.Player, r.Wins, r.Losses, r.FinalKills, r.FinalDeaths, r.BedsBroken}
}

// urchinSessionTokens is the !daily / !weekly / !monthly palette over one
// period's Coral delta. Named rather than inline because the {bw.<period>.…}
// token families render through it too.
func urchinSessionTokens() module.TokenExpander[gossiprpc.UrchinSessionReply] {
	type reply = gossiprpc.UrchinSessionReply
	return bedWarsTokens(sessionBedWars, module.TokenExpander[reply]{
		"games":  func(r *reply) string { return i64(r.GamesPlayed) },
		"levels": func(r *reply) string { return i64(r.Levels) },
	})
}

// urchinStatsRun answers !bwstats with lifetime Bed Wars stats. Template
// tokens: {player} {stars} {wins} {losses} {finals} {finaldeaths} {beds}
// {fkdr} {wlr}.
func urchinStatsRun(d engine.Deps) module.RunFunc {
	return externalCommand[urchinConfig, gossiprpc.HypixelStatsReply]{
		route:    urchinRoute("hypixel", "stats"),
		enabled:  func(c urchinConfig) string { return c.StatsEnabled },
		message:  func(c urchinConfig) string { return c.StatsMessage },
		fallback: defaultUrchinStatsTemplate,
		tokens:   urchinStatsTokens(),
	}.run(d)
}

// urchinStatsTokens is the !bwstats palette over the lifetime Hypixel reply,
// shared with the {bw.…} token family.
func urchinStatsTokens() module.TokenExpander[gossiprpc.HypixelStatsReply] {
	type reply = gossiprpc.HypixelStatsReply
	return bedWarsTokens(statsBedWars, module.TokenExpander[reply]{
		"stars": func(r *reply) string { return i64(r.Stars) },
		"wlr":   func(r *reply) string { return ratio(r.Wins, r.Losses) },
	})
}

// urchinSniperRun answers !sniper with the Urchin (Cubelify overlay) score.
// Template tokens: {player} {score} {mode} {tagcount}.
func urchinSniperRun(d engine.Deps) module.RunFunc {
	return externalCommand[urchinConfig, gossiprpc.UrchinSniperReply]{
		route:    urchinRoute("urchin", "sniper"),
		enabled:  func(c urchinConfig) string { return c.SniperEnabled },
		message:  func(c urchinConfig) string { return c.SniperMessage },
		fallback: defaultUrchinSniperTemplate,
		tokens:   urchinSniperTokens(),
	}.run(d)
}

// urchinSniperTokens is the !sniper palette over the Cubelify overlay score,
// shared with the {urchin.…} token family.
func urchinSniperTokens() module.TokenExpander[gossiprpc.UrchinSniperReply] {
	type reply = gossiprpc.UrchinSniperReply
	return module.TokenExpander[reply]{
		"player":   func(r *reply) string { return r.Player },
		"score":    func(r *reply) string { return trimScore(r.Score) },
		"mode":     func(r *reply) string { return r.Mode },
		"tagcount": func(r *reply) string { return i64(int64(r.TagCount)) },
	}
}

// The Bed Wars token families. {bw.…} is the lifetime profile and
// {bw.daily.…} / {bw.weekly.…} / {bw.monthly.…} the three period deltas;
// {urchin.…} is the overlay score, which is about a player's reputation rather
// than their Bed Wars numbers and so keeps the module's own name.
const (
	bwTokenPrefix     = "bw."
	bwDailyPrefix     = "bw.daily."
	bwWeeklyPrefix    = "bw.weekly."
	bwMonthlyPrefix   = "bw.monthly."
	urchinTokenPrefix = "urchin."
)

// urchinFamilies is this module's contribution: the lifetime profile, the three
// period deltas, and the overlay score.
//
// The tag views (!tag, !tagdescription) get none. Their {tags} is a joined list
// that is the whole message when the command prints it, the same reason
// !valmatches and !crdecks have no family; and a blacklist reputation dropped
// into the middle of a broadcaster's own sentence about a viewer is a line
// nobody should be able to write by accident.
//
// Every family passes preferUUID=true: Hypixel REQUIRES a Mojang uuid and Coral
// accepts one, which is exactly why the module stores it beside the name.
func urchinFamilies() []engine.GameFamilySpec {
	session := urchinSessionTokens()
	return []engine.GameFamilySpec{
		linkedGameFamily[urchinConfig](
			bwTokenPrefix, urchinModuleName, urchinRoute("hypixel", "stats"), urchinStatsTokens(), true).spec(),
		bwSessionFamily(bwDailyPrefix, urchinDailyWindow, session),
		bwSessionFamily(bwWeeklyPrefix, urchinWeeklyWindow, session),
		bwSessionFamily(bwMonthlyPrefix, urchinMonthlyWindow, session),
		linkedGameFamily[urchinConfig](
			urchinTokenPrefix, urchinModuleName, urchinRoute("urchin", "sniper"), urchinSniperTokens(), true).spec(),
	}
}

// bwSessionFamily is one period's family, over the very endpoint its own
// command asks.
func bwSessionFamily(prefix string, w urchinWindow, tokens module.TokenExpander[gossiprpc.UrchinSessionReply]) engine.GameFamilySpec {
	return linkedGameFamily[urchinConfig](
		prefix, urchinModuleName, urchinRoute("urchin", w.endpoint), tokens, true).spec()
}

// urchinTagsRun answers !tag with the player's active blacklist tags (display
// names only, no reason). Template tokens: {player} {tags} {tagcount}.
func urchinTagsRun(d engine.Deps) module.RunFunc {
	return urchinTagRun(d, urchinTagCommand{
		enabled:  func(c urchinConfig) string { return c.TagsEnabled },
		message:  func(c urchinConfig) string { return c.TagsMessage },
		fallback: defaultUrchinTagsTemplate,
		format:   formatUrchinTags,
	})
}

// urchinTagDescriptionRun answers !tagdescription with the same tags including
// the reason (the cleanup version). Template tokens: {player} {tags}
// {tagcount}.
func urchinTagDescriptionRun(d engine.Deps) module.RunFunc {
	return urchinTagRun(d, urchinTagCommand{
		enabled:  func(c urchinConfig) string { return c.TagDescriptionEnabled },
		message:  func(c urchinConfig) string { return c.TagDescriptionMessage },
		fallback: defaultUrchinTagDescriptionTemplate,
		format:   formatUrchinTagDescriptions,
	})
}

// urchinTagCommand is what the two tag commands differ in: their own toggle
// and template, and whether the rendered list carries the reason. Both ask the
// same Coral endpoint.
type urchinTagCommand struct {
	enabled  func(urchinConfig) string
	message  func(urchinConfig) string
	fallback string
	format   func([]gossiprpc.UrchinTag) string
}

// urchinTagRun builds either tag command.
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

// displayTagType maps a Coral API tag_type to a human-readable display name.
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
		// Future-proof: title-case with underscores replaced by spaces.
		s := strings.ReplaceAll(tagType, "_", " ")
		if len(s) > 0 {
			return strings.ToUpper(s[:1]) + s[1:]
		}
		return s
	}
}

// formatUrchinTags renders the tag list for chat with display names only:
// "Blatant Cheater, Sniper", or "No tags" when the player has none.
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

// formatUrchinTagDescriptions renders the tag list with display names and
// reasons: "Blatant Cheater (bhop), Sniper", or "No tags" when empty.
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
