package modules

import (
	"context"
	"fmt"
	"strings"
	"time"

	"ItsBagelBot/app/sesame/engine"
	"ItsBagelBot/app/sesame/module"
	"ItsBagelBot/internal/domain/outgress"
	gatewayrpc "ItsBagelBot/internal/domain/rpc/gateway"

	"go.uber.org/zap"
)

// urchinModuleName is the ModuleView key; the console MODULE_CATALOG entry and
// the dashboard module page use the same id.
const urchinModuleName = "urchin"

// urchinCooldown is the shared per-command window; the gateway caches upstream
// replies, so this only shields chat from command spam, not the API.
const urchinCooldown = 10 * time.Second

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
// default account (blank = the broadcaster's own Twitch login). Each *Enabled
// is a per-command toggle stored "on"/"off" — empty means on, matching the
// alerts module's semantics — and each *Message is a customized template
// (blank = default).
type urchinConfig struct {
	Account string `json:"account"`

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
// Coral API through the gateway service. It is a named, opt-in module
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
		Run(urchinSessionRun(d, "daily"))
	m.Command("weekly").Everyone().Cooldown(urchinCooldown).Aliases("bwweekly").
		Run(urchinSessionRun(d, "weekly"))
	m.Command("monthly").Everyone().Cooldown(urchinCooldown).Aliases("bwmonthly").
		Run(urchinSessionRun(d, "monthly"))
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

// urchinToggle returns one command's (enabled, template, default) triple from
// the decoded config.
func urchinToggle(cfg urchinConfig, endpoint string) (enabled bool, tmpl string) {
	switch endpoint {
	case "daily":
		return alertOn(cfg.DailyEnabled), orDefault(cfg.DailyMessage, defaultUrchinDailyTemplate)
	case "weekly":
		return alertOn(cfg.WeeklyEnabled), orDefault(cfg.WeeklyMessage, defaultUrchinWeeklyTemplate)
	case "monthly":
		return alertOn(cfg.MonthlyEnabled), orDefault(cfg.MonthlyMessage, defaultUrchinMonthlyTemplate)
	case "stats":
		return alertOn(cfg.StatsEnabled), orDefault(cfg.StatsMessage, defaultUrchinStatsTemplate)
	case "sniper":
		return alertOn(cfg.SniperEnabled), orDefault(cfg.SniperMessage, defaultUrchinSniperTemplate)
	case "tags":
		return alertOn(cfg.TagsEnabled), orDefault(cfg.TagsMessage, defaultUrchinTagsTemplate)
	case "tagdescription":
		return alertOn(cfg.TagDescriptionEnabled), orDefault(cfg.TagDescriptionMessage, defaultUrchinTagDescriptionTemplate)
	default:
		return false, ""
	}
}

// gatewayCommand names one urchin command's wiring: the config toggle key and
// the gateway provider/endpoint it calls.
type gatewayCommand struct {
	toggle   string
	provider string
	endpoint string
}

// runUrchinCommand is the shared skeleton every urchin command runs: decode
// the channel config, check the command's toggle, resolve the target account,
// call the gateway, then expand the reply's tokens into the template. tokens
// maps a template key to its reply field; unknown keys fall through to the
// dynamic palette.
func runUrchinCommand[R any](d engine.Deps, cmd gatewayCommand, tokens map[string]func(*R) string) module.RunFunc {
	return func(ctx context.Context, c *module.Context, args string, emit module.Emit) error {
		var cfg urchinConfig
		_ = c.Decode(&cfg)
		enabled, tmpl := urchinToggle(cfg, cmd.toggle)
		if !enabled || d.Gateway == nil {
			return nil
		}

		account := resolveAccount(args, cfg.Account, c.Env.BroadcasterUserLogin)
		var reply R
		if err := d.Gateway.Call(ctx, cmd.provider, cmd.endpoint, gatewayrpc.Request{Account: account, IsPremium: c.Regress.IsPremium()}, &reply); err != nil {
			if chatReplyError(c, emit, account, err) {
				return nil
			}
			return err
		}

		msg := module.ExpandString(tmpl, func(key string) (string, bool) {
			if field, ok := tokens[key]; ok {
				return field(&reply), true
			}
			return module.ParseDynamic(key)
		})
		emit(&module.Output{Type: outgress.TypeChat, BroadcasterID: c.Env.BroadcasterUserID, Text: msg})
		return nil
	}
}

// urchinSessionRun answers !daily / !weekly / !monthly with the period's Bed
// Wars delta. Template tokens: {player} {wins} {losses} {finals} {finaldeaths}
// {beds} {games} {levels} {fkdr}.
func urchinSessionRun(d engine.Deps, endpoint string) module.RunFunc {
	type reply = gatewayrpc.UrchinSessionReply
	return runUrchinCommand(d, gatewayCommand{endpoint, "urchin", endpoint}, map[string]func(*reply) string{
		"player":      func(r *reply) string { return r.Player },
		"wins":        func(r *reply) string { return i64(r.Wins) },
		"losses":      func(r *reply) string { return i64(r.Losses) },
		"finals":      func(r *reply) string { return i64(r.FinalKills) },
		"finaldeaths": func(r *reply) string { return i64(r.FinalDeaths) },
		"beds":        func(r *reply) string { return i64(r.BedsBroken) },
		"games":       func(r *reply) string { return i64(r.GamesPlayed) },
		"levels":      func(r *reply) string { return i64(r.Levels) },
		"fkdr":        func(r *reply) string { return ratio(r.FinalKills, r.FinalDeaths) },
	})
}

// urchinStatsRun answers !bwstats with lifetime Bed Wars stats. Template
// tokens: {player} {stars} {wins} {losses} {finals} {finaldeaths} {beds}
// {fkdr} {wlr}.
//
// The data rides the gateway's hypixel provider — a separate external system
// with its own key and budget (Coral cannot serve lifetime stats on our key) —
// but the command stays on the one urchin module page: gateway provider layout
// is not a dashboard concern.
func urchinStatsRun(d engine.Deps) module.RunFunc {
	type reply = gatewayrpc.HypixelStatsReply
	return runUrchinCommand(d, gatewayCommand{"stats", "hypixel", "stats"}, map[string]func(*reply) string{
		"player":      func(r *reply) string { return r.Player },
		"stars":       func(r *reply) string { return i64(r.Stars) },
		"wins":        func(r *reply) string { return i64(r.Wins) },
		"losses":      func(r *reply) string { return i64(r.Losses) },
		"finals":      func(r *reply) string { return i64(r.FinalKills) },
		"finaldeaths": func(r *reply) string { return i64(r.FinalDeaths) },
		"beds":        func(r *reply) string { return i64(r.BedsBroken) },
		"fkdr":        func(r *reply) string { return ratio(r.FinalKills, r.FinalDeaths) },
		"wlr":         func(r *reply) string { return ratio(r.Wins, r.Losses) },
	})
}

// urchinSniperRun answers !sniper with the Urchin (Cubelify overlay) score.
// Template tokens: {player} {score} {mode} {tagcount}.
func urchinSniperRun(d engine.Deps) module.RunFunc {
	type reply = gatewayrpc.UrchinSniperReply
	return runUrchinCommand(d, gatewayCommand{"sniper", "urchin", "sniper"}, map[string]func(*reply) string{
		"player":   func(r *reply) string { return r.Player },
		"score":    func(r *reply) string { return trimScore(r.Score) },
		"mode":     func(r *reply) string { return r.Mode },
		"tagcount": func(r *reply) string { return i64(int64(r.TagCount)) },
	})
}

// urchinTagsRun answers !tag with the player's active blacklist tags (display
// names only, no reason). Template tokens: {player} {tags} {tagcount}.
func urchinTagsRun(d engine.Deps) module.RunFunc {
	return runUrchinCommand(d, gatewayCommand{"tags", "urchin", "tags"}, tagTokens(formatUrchinTags))
}

// urchinTagDescriptionRun answers !tagdescription with the player's active
// blacklist tags including the reason (the cleanup version).
// Template tokens: {player} {tags} {tagcount}.
func urchinTagDescriptionRun(d engine.Deps) module.RunFunc {
	return runUrchinCommand(d, gatewayCommand{"tagdescription", "urchin", "tags"}, tagTokens(formatUrchinTagDescriptions))
}

// tagTokens builds the token set both tag commands share; format renders the
// tag list (with or without reasons).
func tagTokens(format func([]gatewayrpc.UrchinTag) string) map[string]func(*gatewayrpc.UrchinTagsReply) string {
	type reply = gatewayrpc.UrchinTagsReply
	return map[string]func(*reply) string{
		"player":   func(r *reply) string { return r.Player },
		"tags":     func(r *reply) string { return format(r.Tags) },
		"tagcount": func(r *reply) string { return i64(int64(len(r.Tags))) },
	}
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
func formatUrchinTags(tags []gatewayrpc.UrchinTag) string {
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
func formatUrchinTagDescriptions(tags []gatewayrpc.UrchinTag) string {
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
