package modules

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"

	"ItsBagelBot/app/sesame/engine"
	"ItsBagelBot/app/sesame/module"
	"ItsBagelBot/internal/domain/outgress"
)

// Default chat templates for each alert. Tokens are documented per handler below.
const (
	defaultFollowTemplate = "🥯 Thanks for the follow, {user}!"
	defaultSubTemplate    = "🥯 {user} just subscribed! Welcome to the sub squad!"
	defaultCheerTemplate  = "🥯 {user} cheered {bits} bits! Thanks for the support!"
	defaultRaidTemplate   = "🥯 {user} is raiding with {viewers} viewers!"
)

// alertsConfig holds the broadcaster's per-alert enable flags and customized
// templates. Each *Enabled is a dashboard toggle stored as "on"/"off"; empty
// (no stored value) means default-on, so a freshly enabled module fires every
// alert until the broadcaster turns one off. Each *Message empty falls back to
// that alert's default template.
type alertsConfig struct {
	FollowEnabled string `json:"followEnabled"`
	FollowMessage string `json:"followMessage"`
	SubEnabled    string `json:"subEnabled"`
	SubMessage    string `json:"subMessage"`
	CheerEnabled  string `json:"cheerEnabled"`
	CheerMessage  string `json:"cheerMessage"`
	RaidEnabled   string `json:"raidEnabled"`
	RaidMessage   string `json:"raidMessage"`
}

// alertOn reports whether a sub-alert toggle is on. Only an explicit "off"
// disables it; empty (never set) and "on" both fire, so each alert defaults on.
func alertOn(v string) bool { return v != "off" }

// followEvent is the subset of the channel.follow EventSub payload we use.
type followEvent struct {
	UserName          string `json:"user_name"`
	UserLogin         string `json:"user_login"`
	BroadcasterUserID string `json:"broadcaster_user_id"`
}

// subscribeEvent is the subset of the channel.subscribe EventSub payload we use.
type subscribeEvent struct {
	UserName          string `json:"user_name"`
	UserLogin         string `json:"user_login"`
	BroadcasterUserID string `json:"broadcaster_user_id"`
	Tier              string `json:"tier"`
}

// cheerEvent is the subset of the channel.cheer EventSub payload we use. A
// cheer left anonymous by the chatter carries no user identity.
type cheerEvent struct {
	IsAnonymous       bool   `json:"is_anonymous"`
	UserName          string `json:"user_name"`
	UserLogin         string `json:"user_login"`
	BroadcasterUserID string `json:"broadcaster_user_id"`
	Bits              int    `json:"bits"`
}

// Alerts posts a chat line on channel.follow, channel.subscribe, channel.cheer
// and channel.raid. It is a named, default-on module (KindDefault): it ships
// enabled and runs unless the broadcaster disables the whole module on the
// dashboard. Each alert has its own enable toggle and message template, wired in
// from the module config the pipeline sets on the Context. Raid is a separate
// alert from the Shoutout module: Shoutout points chat at the raider's channel,
// this just announces the raid happened.
func Alerts(_ engine.Deps) module.Module {
	m := module.NewModule("alerts", module.KindDefault)

	m.On("channel.follow", func(_ context.Context, c *module.Context, emit module.Emit) error {
		var cfg alertsConfig
		_ = c.Decode(&cfg)
		if !alertOn(cfg.FollowEnabled) {
			return nil
		}
		if len(c.Env.Event) == 0 {
			return nil
		}
		var ev followEvent
		if err := json.Unmarshal(c.Env.Event, &ev); err != nil {
			return err
		}
		if ev.UserLogin == "" {
			return nil
		}

		tmpl := cfg.FollowMessage
		if tmpl == "" {
			tmpl = defaultFollowTemplate
		}

		emit(&module.Output{
			Type:          outgress.TypeChat,
			BroadcasterID: ev.BroadcasterUserID,
			Text:          expandUserTokens(tmpl, displayName(ev.UserName, ev.UserLogin), ev.UserLogin),
		})
		return nil
	})

	m.On("channel.subscribe", func(_ context.Context, c *module.Context, emit module.Emit) error {
		var cfg alertsConfig
		_ = c.Decode(&cfg)
		if !alertOn(cfg.SubEnabled) {
			return nil
		}
		if len(c.Env.Event) == 0 {
			return nil
		}
		var ev subscribeEvent
		if err := json.Unmarshal(c.Env.Event, &ev); err != nil {
			return err
		}
		if ev.UserLogin == "" {
			return nil
		}

		tmpl := cfg.SubMessage
		if tmpl == "" {
			tmpl = defaultSubTemplate
		}

		msg := strings.NewReplacer(
			"{user}", displayName(ev.UserName, ev.UserLogin),
			"{user_login}", ev.UserLogin,
			"{tier}", ev.Tier,
		).Replace(tmpl)

		emit(&module.Output{
			Type:          outgress.TypeChat,
			BroadcasterID: ev.BroadcasterUserID,
			Text:          msg,
		})
		return nil
	})

	m.On("channel.cheer", func(_ context.Context, c *module.Context, emit module.Emit) error {
		var cfg alertsConfig
		_ = c.Decode(&cfg)
		if !alertOn(cfg.CheerEnabled) {
			return nil
		}
		if len(c.Env.Event) == 0 {
			return nil
		}
		var ev cheerEvent
		if err := json.Unmarshal(c.Env.Event, &ev); err != nil {
			return err
		}
		if ev.BroadcasterUserID == "" {
			return nil
		}

		tmpl := cfg.CheerMessage
		if tmpl == "" {
			tmpl = defaultCheerTemplate
		}

		cheerer := "An anonymous cheerer"
		login := ev.UserLogin
		if !ev.IsAnonymous {
			cheerer = displayName(ev.UserName, ev.UserLogin)
		}

		msg := strings.NewReplacer(
			"{user}", cheerer,
			"{user_login}", login,
			"{bits}", strconv.Itoa(ev.Bits),
		).Replace(tmpl)

		emit(&module.Output{
			Type:          outgress.TypeChat,
			BroadcasterID: ev.BroadcasterUserID,
			Text:          msg,
		})
		return nil
	})

	m.On("channel.raid", func(_ context.Context, c *module.Context, emit module.Emit) error {
		var cfg alertsConfig
		_ = c.Decode(&cfg)
		if !alertOn(cfg.RaidEnabled) {
			return nil
		}
		if len(c.Env.Event) == 0 {
			return nil
		}
		var ev raidEvent
		if err := json.Unmarshal(c.Env.Event, &ev); err != nil {
			return err
		}
		if ev.FromBroadcasterUserLogin == "" {
			return nil
		}

		tmpl := cfg.RaidMessage
		if tmpl == "" {
			tmpl = defaultRaidTemplate
		}

		msg := strings.NewReplacer(
			"{user}", displayName(ev.FromBroadcasterUserName, ev.FromBroadcasterUserLogin),
			"{user_login}", ev.FromBroadcasterUserLogin,
			"{viewers}", strconv.Itoa(ev.Viewers),
		).Replace(tmpl)

		emit(&module.Output{
			Type:          outgress.TypeChat,
			BroadcasterID: ev.ToBroadcasterUserID,
			Text:          msg,
		})
		return nil
	})

	return m.Build()
}

// displayName prefers the EventSub display name, falling back to the login
// when Twitch omits it.
func displayName(name, login string) string {
	if name != "" {
		return name
	}
	return login
}

// expandUserTokens replaces {user} and {user_login} in tmpl.
func expandUserTokens(tmpl, user, login string) string {
	return strings.NewReplacer("{user}", user, "{user_login}", login).Replace(tmpl)
}
