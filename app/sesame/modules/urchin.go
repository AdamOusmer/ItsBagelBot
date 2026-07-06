package modules

import (
	"context"
	"strings"
	"time"

	"ItsBagelBot/app/sesame/engine"
	"ItsBagelBot/app/sesame/module"
	gatewayrpc "ItsBagelBot/internal/domain/rpc/gateway"
	"ItsBagelBot/internal/domain/outgress"

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
	defaultUrchinDailyTemplate   = "{player} today: {wins}W {losses}L · {finals} finals · {beds} beds · {fkdr} FKDR"
	defaultUrchinWeeklyTemplate  = "{player} this week: {wins}W {losses}L · {finals} finals · {beds} beds · {fkdr} FKDR"
	defaultUrchinMonthlyTemplate = "{player} this month: {wins}W {losses}L · {finals} finals · {beds} beds · {fkdr} FKDR"
	defaultUrchinStatsTemplate   = "{player}: {stars} stars · {wins} wins · {finals} finals · {fkdr} FKDR · {beds} beds broken"
	defaultUrchinSniperTemplate  = "{player} urchin score: {score}"
	defaultUrchinTagsTemplate    = "{player}: {tags}"
)

// urchinConfig is the module's dashboard configuration. Account is the linked
// default account (blank = the broadcaster's own Twitch login). Each *Enabled
// is a per-command toggle stored "on"/"off" — empty means on, matching the
// alerts module's semantics — and each *Message is a customized template
// (blank = default).
type urchinConfig struct {
	Account string `json:"account"`

	DailyEnabled   string `json:"dailyEnabled"`
	DailyMessage   string `json:"dailyMessage"`
	WeeklyEnabled  string `json:"weeklyEnabled"`
	WeeklyMessage  string `json:"weeklyMessage"`
	MonthlyEnabled string `json:"monthlyEnabled"`
	MonthlyMessage string `json:"monthlyMessage"`
	StatsEnabled   string `json:"statsEnabled"`
	StatsMessage   string `json:"statsMessage"`
	SniperEnabled  string `json:"sniperEnabled"`
	SniperMessage  string `json:"sniperMessage"`
	TagsEnabled    string `json:"tagsEnabled"`
	TagsMessage    string `json:"tagsMessage"`
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
	m.Command("tags").Everyone().Cooldown(urchinCooldown).Aliases("bwtags").
		Run(urchinTagsRun(d))
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
	default:
		return false, ""
	}
}

// urchinSessionRun answers !daily / !weekly / !monthly with the period's Bed
// Wars delta. Template tokens: {player} {wins} {losses} {finals} {finaldeaths}
// {beds} {games} {levels} {fkdr}.
func urchinSessionRun(d engine.Deps, endpoint string) module.RunFunc {
	return func(ctx context.Context, c *module.Context, args string, emit module.Emit) error {
		var cfg urchinConfig
		_ = c.Decode(&cfg)
		enabled, tmpl := urchinToggle(cfg, endpoint)
		if !enabled || d.Gateway == nil {
			return nil
		}

		account := resolveAccount(args, cfg.Account, c.Env.BroadcasterUserLogin)
		var reply gatewayrpc.UrchinSessionReply
		if err := d.Gateway.Call(ctx, "urchin", endpoint, gatewayrpc.Request{Account: account}, &reply); err != nil {
			if chatReplyError(c, emit, account, err) {
				return nil
			}
			return err
		}

		msg := module.ExpandString(tmpl, func(key string) (string, bool) {
			switch key {
			case "player":
				return reply.Player, true
			case "wins":
				return i64(reply.Wins), true
			case "losses":
				return i64(reply.Losses), true
			case "finals":
				return i64(reply.FinalKills), true
			case "finaldeaths":
				return i64(reply.FinalDeaths), true
			case "beds":
				return i64(reply.BedsBroken), true
			case "games":
				return i64(reply.GamesPlayed), true
			case "levels":
				return i64(reply.Levels), true
			case "fkdr":
				return ratio(reply.FinalKills, reply.FinalDeaths), true
			default:
				return module.ParseDynamic(key)
			}
		})
		emit(&module.Output{Type: outgress.TypeChat, BroadcasterID: c.Env.BroadcasterUserID, Text: msg})
		return nil
	}
}

// urchinStatsRun answers !bwstats with lifetime Bed Wars stats. Template
// tokens: {player} {stars} {wins} {losses} {finals} {finaldeaths} {beds}
// {fkdr} {wlr}.
func urchinStatsRun(d engine.Deps) module.RunFunc {
	return func(ctx context.Context, c *module.Context, args string, emit module.Emit) error {
		var cfg urchinConfig
		_ = c.Decode(&cfg)
		enabled, tmpl := urchinToggle(cfg, "stats")
		if !enabled || d.Gateway == nil {
			return nil
		}

		account := resolveAccount(args, cfg.Account, c.Env.BroadcasterUserLogin)
		var reply gatewayrpc.UrchinStatsReply
		if err := d.Gateway.Call(ctx, "urchin", "stats", gatewayrpc.Request{Account: account}, &reply); err != nil {
			if chatReplyError(c, emit, account, err) {
				return nil
			}
			return err
		}

		msg := module.ExpandString(tmpl, func(key string) (string, bool) {
			switch key {
			case "player":
				return reply.Player, true
			case "stars":
				return i64(reply.Stars), true
			case "wins":
				return i64(reply.Wins), true
			case "losses":
				return i64(reply.Losses), true
			case "finals":
				return i64(reply.FinalKills), true
			case "finaldeaths":
				return i64(reply.FinalDeaths), true
			case "beds":
				return i64(reply.BedsBroken), true
			case "fkdr":
				return ratio(reply.FinalKills, reply.FinalDeaths), true
			case "wlr":
				return ratio(reply.Wins, reply.Losses), true
			default:
				return module.ParseDynamic(key)
			}
		})
		emit(&module.Output{Type: outgress.TypeChat, BroadcasterID: c.Env.BroadcasterUserID, Text: msg})
		return nil
	}
}

// urchinSniperRun answers !sniper with the Urchin (Cubelify overlay) score.
// Template tokens: {player} {score} {mode} {tagcount}.
func urchinSniperRun(d engine.Deps) module.RunFunc {
	return func(ctx context.Context, c *module.Context, args string, emit module.Emit) error {
		var cfg urchinConfig
		_ = c.Decode(&cfg)
		enabled, tmpl := urchinToggle(cfg, "sniper")
		if !enabled || d.Gateway == nil {
			return nil
		}

		account := resolveAccount(args, cfg.Account, c.Env.BroadcasterUserLogin)
		var reply gatewayrpc.UrchinSniperReply
		if err := d.Gateway.Call(ctx, "urchin", "sniper", gatewayrpc.Request{Account: account}, &reply); err != nil {
			if chatReplyError(c, emit, account, err) {
				return nil
			}
			return err
		}

		msg := module.ExpandString(tmpl, func(key string) (string, bool) {
			switch key {
			case "player":
				return reply.Player, true
			case "score":
				return trimScore(reply.Score), true
			case "mode":
				return reply.Mode, true
			case "tagcount":
				return i64(int64(reply.TagCount)), true
			default:
				return module.ParseDynamic(key)
			}
		})
		emit(&module.Output{Type: outgress.TypeChat, BroadcasterID: c.Env.BroadcasterUserID, Text: msg})
		return nil
	}
}

// urchinTagsRun answers !tags with the player's active blacklist tags.
// Template tokens: {player} {tags} {tagcount}.
func urchinTagsRun(d engine.Deps) module.RunFunc {
	return func(ctx context.Context, c *module.Context, args string, emit module.Emit) error {
		var cfg urchinConfig
		_ = c.Decode(&cfg)
		enabled, tmpl := urchinToggle(cfg, "tags")
		if !enabled || d.Gateway == nil {
			return nil
		}

		account := resolveAccount(args, cfg.Account, c.Env.BroadcasterUserLogin)
		var reply gatewayrpc.UrchinTagsReply
		if err := d.Gateway.Call(ctx, "urchin", "tags", gatewayrpc.Request{Account: account}, &reply); err != nil {
			if chatReplyError(c, emit, account, err) {
				return nil
			}
			return err
		}

		msg := module.ExpandString(tmpl, func(key string) (string, bool) {
			switch key {
			case "player":
				return reply.Player, true
			case "tags":
				return formatUrchinTags(reply.Tags), true
			case "tagcount":
				return i64(int64(len(reply.Tags))), true
			default:
				return module.ParseDynamic(key)
			}
		})
		emit(&module.Output{Type: outgress.TypeChat, BroadcasterID: c.Env.BroadcasterUserID, Text: msg})
		return nil
	}
}

// formatUrchinTags renders the tag list for chat: "cheater (bhop), sniper", or
// "no tags — clean" when the player has none.
func formatUrchinTags(tags []gatewayrpc.UrchinTag) string {
	if len(tags) == 0 {
		return "no tags — clean"
	}
	parts := make([]string, 0, len(tags))
	for _, t := range tags {
		if t.Reason != "" {
			parts = append(parts, t.Type+" ("+t.Reason+")")
		} else {
			parts = append(parts, t.Type)
		}
	}
	return strings.Join(parts, ", ")
}
