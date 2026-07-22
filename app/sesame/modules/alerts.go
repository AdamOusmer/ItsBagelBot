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
	defaultFollowTemplate = "Thank you for following the channel, {user}!"
	defaultSubTemplate    = "Welcome to the community, {user}! Thank you for subscribing!"
	defaultGiftTemplate   = "{user} just gifted {count} subs to the community! Thank you!"
	defaultCheerTemplate  = "Thank you for the {bits} bits, {user}!"
	defaultRaidTemplate   = "{user} is raiding the channel with {viewers} viewers! Welcome everyone!"
	defaultAdsTemplate    = "Ads are rolling for {duration} seconds. Hang tight, we'll be right back!"
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
	GiftEnabled   string `json:"giftEnabled"`
	GiftMessage   string `json:"giftMessage"`
	CheerEnabled  string `json:"cheerEnabled"`
	CheerMessage  string `json:"cheerMessage"`
	RaidEnabled   string `json:"raidEnabled"`
	RaidMessage   string `json:"raidMessage"`
	// AdsEnabled is the one default-OFF toggle in the module: unlike the
	// alerts above, it fires only on an explicit "on" (see adAlertOn), so
	// enabling the module never starts announcing ad breaks by surprise.
	AdsEnabled string `json:"adsEnabled"`
	AdsMessage string `json:"adsMessage"`
}

// alertOn reports whether a sub-alert toggle is on. Only an explicit "off"
// disables it; empty (never set) and "on" both fire, so each alert defaults on.
func alertOn(v string) bool { return v != "off" }

// adAlertOn is the inverse posture for the ads alert: it stays silent until
// the broadcaster explicitly turns it on, so only "on" fires.
func adAlertOn(v string) bool { return v == "on" }

// followEvent is the subset of the channel.follow EventSub payload we use.
type followEvent struct {
	UserName          string `json:"user_name"`
	UserLogin         string `json:"user_login"`
	BroadcasterUserID string `json:"broadcaster_user_id"`
}

// subscribeEvent is the subset of the channel.subscribe EventSub payload we
// use. IsGift marks a gifted recipient: Twitch fires one channel.subscribe per
// recipient of a gift, on top of the single channel.subscription.gift for the
// gifter.
type subscribeEvent struct {
	UserName          string `json:"user_name"`
	UserLogin         string `json:"user_login"`
	BroadcasterUserID string `json:"broadcaster_user_id"`
	Tier              string `json:"tier"`
	IsGift            bool   `json:"is_gift"`
}

// giftEvent is the subset of the channel.subscription.gift EventSub payload we
// use. A gift left anonymous by the gifter carries no user identity.
type giftEvent struct {
	IsAnonymous       bool   `json:"is_anonymous"`
	UserName          string `json:"user_name"`
	UserLogin         string `json:"user_login"`
	BroadcasterUserID string `json:"broadcaster_user_id"`
	Total             int    `json:"total"`
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

// adBreakEvent is the subset of the channel.ad_break.begin EventSub payload we
// use.
type adBreakEvent struct {
	BroadcasterUserID string `json:"broadcaster_user_id"`
	DurationSeconds   int    `json:"duration_seconds"`
}

// Alerts posts a chat line on channel.follow, channel.subscribe,
// channel.subscription.gift, channel.cheer, channel.raid and
// channel.ad_break.begin. It is a named, default-on module
// (KindDefault): it ships
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

		msg := module.ExpandString(tmpl, func(key string) (string, bool) {
			switch key {
			case "user":
				return strings.TrimPrefix(displayName(ev.UserName, ev.UserLogin), "@"), true
			default:
				return module.ParseDynamic(key)
			}
		})

		emit(&module.Output{
			Type:          outgress.TypeChat,
			BroadcasterID: ev.BroadcasterUserID,
			Text:          msg,
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
		// A gifted recipient is announced through the gift alert on
		// channel.subscription.gift (one line per gifter, not one per
		// recipient), so a gift bomb cannot flood chat with welcome lines.
		if ev.UserLogin == "" || ev.IsGift {
			return nil
		}

		tmpl := cfg.SubMessage
		if tmpl == "" {
			tmpl = defaultSubTemplate
		}

		msg := module.ExpandString(tmpl, func(key string) (string, bool) {
			switch key {
			case "user":
				return strings.TrimPrefix(displayName(ev.UserName, ev.UserLogin), "@"), true
			case "tier":
				return ev.Tier, true
			default:
				return module.ParseDynamic(key)
			}
		})

		emit(&module.Output{
			Type:          outgress.TypeChat,
			BroadcasterID: ev.BroadcasterUserID,
			Text:          msg,
		})
		return nil
	})

	m.On("channel.subscription.gift", func(_ context.Context, c *module.Context, emit module.Emit) error {
		var cfg alertsConfig
		_ = c.Decode(&cfg)
		if !alertOn(cfg.GiftEnabled) {
			return nil
		}
		if len(c.Env.Event) == 0 {
			return nil
		}
		var ev giftEvent
		if err := json.Unmarshal(c.Env.Event, &ev); err != nil {
			return err
		}
		if ev.BroadcasterUserID == "" || ev.Total <= 0 {
			return nil
		}

		tmpl := cfg.GiftMessage
		if tmpl == "" {
			tmpl = defaultGiftTemplate
		}

		gifter := "An anonymous gifter"
		if !ev.IsAnonymous {
			gifter = displayName(ev.UserName, ev.UserLogin)
		}

		msg := module.ExpandString(tmpl, func(key string) (string, bool) {
			switch key {
			case "user":
				return strings.TrimPrefix(gifter, "@"), true
			case "count":
				return strconv.Itoa(ev.Total), true
			case "tier":
				return ev.Tier, true
			default:
				return module.ParseDynamic(key)
			}
		})

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
		if !ev.IsAnonymous {
			cheerer = displayName(ev.UserName, ev.UserLogin)
		}

		msg := module.ExpandString(tmpl, func(key string) (string, bool) {
			switch key {
			case "user":
				return strings.TrimPrefix(cheerer, "@"), true
			case "bits":
				return strconv.Itoa(ev.Bits), true
			default:
				return module.ParseDynamic(key)
			}
		})

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

		msg := module.ExpandString(tmpl, func(key string) (string, bool) {
			switch key {
			case "user":
				return strings.TrimPrefix(displayName(ev.FromBroadcasterUserName, ev.FromBroadcasterUserLogin), "@"), true
			case "viewers":
				return strconv.Itoa(ev.Viewers), true
			default:
				return module.ParseDynamic(key)
			}
		})

		emit(&module.Output{
			Type:          outgress.TypeChat,
			BroadcasterID: ev.ToBroadcasterUserID,
			Text:          msg,
		})
		return nil
	})

	m.On("channel.ad_break.begin", func(_ context.Context, c *module.Context, emit module.Emit) error {
		var cfg alertsConfig
		_ = c.Decode(&cfg)
		if !adAlertOn(cfg.AdsEnabled) {
			return nil
		}
		if len(c.Env.Event) == 0 {
			return nil
		}
		var ev adBreakEvent
		if err := json.Unmarshal(c.Env.Event, &ev); err != nil {
			return err
		}
		if ev.BroadcasterUserID == "" {
			return nil
		}

		tmpl := cfg.AdsMessage
		if tmpl == "" {
			tmpl = defaultAdsTemplate
		}

		msg := module.ExpandString(tmpl, func(key string) (string, bool) {
			switch key {
			case "duration":
				return strconv.Itoa(ev.DurationSeconds), true
			default:
				return module.ParseDynamic(key)
			}
		})

		emit(&module.Output{
			Type:          outgress.TypeChat,
			BroadcasterID: ev.BroadcasterUserID,
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

