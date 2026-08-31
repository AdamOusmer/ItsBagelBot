// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package modules

import (
	"context"
	"strconv"
	"strings"
	"time"

	"ItsBagelBot/app/sesame/engine"
	"ItsBagelBot/app/sesame/module"
	"ItsBagelBot/internal/activity"
	"ItsBagelBot/internal/domain/outgress"
	"ItsBagelBot/pkg/codec"

	"go.uber.org/zap"
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

// followAlertWindow is how long a channel remembers that it already thanked a
// follower. Twitch re-sends channel.follow on every re-follow, so without this
// a viewer can unfollow/refollow in a loop and drive the alert line as fast as
// they can click. Three days is well past the timescale that baiting is fun on,
// and it also swallows the duplicate a JetStream redelivery would otherwise
// turn into a second thank-you.
//
// The window is deliberately per (channel, follower): a genuine follower who
// leaves and comes back later in the week is still thanked, and one channel's
// alerts never suppress another's. See followAlertKey for the Valkey key, and
// docs/src/content/docs/microservices/sesame.md ("Follow-alert dedupe") for why
// a multi-day window is safe to hold in the keyspace.
const followAlertWindow = 72 * time.Hour

// followEvent is the subset of the channel.follow EventSub payload we use.
type followEvent struct {
	UserID            string `json:"user_id"`
	UserName          string `json:"user_name"`
	UserLogin         string `json:"user_login"`
	BroadcasterUserID string `json:"broadcaster_user_id"`
}

// followAlertKey is the per-channel, per-follower claim the alert takes for
// followAlertWindow. Both ids are Twitch's numeric user ids, so a rename never
// re-opens the window the way a login-keyed claim would.
func followAlertKey(ev followEvent) string {
	return "alert:follow:" + ev.BroadcasterUserID + ":" + ev.UserID
}

// firstFollowAlert claims this follower's window and reports whether the alert
// should fire. It runs only once the alert is known to be enabled, so a channel
// with follow alerts off never burns a window it would want later.
//
// It fails open: with no cooldown store wired, no user id on the payload, or
// Valkey unreachable, the alert still posts. A missed thank-you is a worse
// outcome than the duplicate an outage can let through, and the abuse this gate
// exists for cannot cause the outage.
func firstFollowAlert(ctx context.Context, cd engine.CooldownStore, log *zap.Logger, ev followEvent) bool {
	if cd == nil || ev.UserID == "" {
		return true
	}
	ok, err := cd.Allow(ctx, followAlertKey(ev), followAlertWindow)
	if err != nil {
		log.Warn("alerts: follow cooldown unavailable, alerting anyway",
			zap.String("broadcaster_id", ev.BroadcasterUserID),
			zap.String("user_id", ev.UserID),
			zap.Error(err),
		)
		return true
	}
	return ok
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

// alertLine is one rendered alert: the channel it goes to and the token
// values its template may expand.
type alertLine struct {
	broadcasterID string
	tokens        map[string]string
}

// onAlert builds the shared handler shell every alert uses: read the toggle
// and custom template out of the module config, decode the event subset, ask
// render for the destination and token values, and emit the expanded line.
// pick returns the alert's enable state and custom template; fallback is the
// default template used when the broadcaster has not set one. render's false
// return drops the event (missing identity, an alert another handler owns, or a
// dedupe window this event lost); it receives the message context so a render
// that has to claim shared state does so only for an alert that is enabled and
// otherwise ready to fire.
func onAlert[T any](pick func(alertsConfig) (bool, string), fallback string, render func(ctx context.Context, ev T) (alertLine, bool)) module.EventHandler {
	return func(ctx context.Context, c *module.Context, emit module.Emit) error {
		var cfg alertsConfig
		_ = c.Decode(&cfg)
		enabled, tmpl := pick(cfg)
		if !enabled || len(c.Env.Event) == 0 {
			return nil
		}
		var ev T
		if err := codec.Unmarshal(c.Env.Event, &ev); err != nil {
			return err
		}
		line, ok := render(ctx, ev)
		if !ok {
			return nil
		}
		if tmpl == "" {
			tmpl = fallback
		}
		msg := module.ExpandString(tmpl, func(key string) (string, bool) {
			if v, found := line.tokens[key]; found {
				return v, true
			}
			return module.ParseDynamic(key)
		})
		emit(&module.Output{
			Type:          outgress.TypeChat,
			BroadcasterID: line.broadcasterID,
			Text:          msg,
		})
		return nil
	}
}

// Alerts posts a chat line on channel.follow, channel.subscribe,
// channel.subscription.message, channel.subscription.gift, channel.cheer,
// channel.raid and channel.ad_break.begin. It is a named, default-on module
// (KindDefault): it ships
// enabled and runs unless the broadcaster disables the whole module on the
// dashboard. Each alert has its own enable toggle and message template, wired in
// from the module config the pipeline sets on the Context. Raid is a separate
// alert from the Shoutout module: Shoutout points chat at the raider's channel,
// this just announces the raid happened.
//
// Only the follow alert is deduplicated (followAlertWindow): it is the one
// event a viewer can re-trigger at will. Subs, gifts, cheers and raids each
// cost the sender something, and ad breaks are the channel's own, so those are
// announced every time.
func Alerts(d engine.Deps) module.Module {
	m := module.NewModule("alerts", module.KindDefault)

	m.On("channel.follow", onAlert(
		func(cfg alertsConfig) (bool, string) { return alertOn(cfg.FollowEnabled), cfg.FollowMessage },
		defaultFollowTemplate,
		followLine(d.Cooldown, moduleLog(d))))

	// The sub alert serves both channel.subscribe (new subs) and
	// channel.subscription.message (resubs shared in chat), so a renewing sub
	// gets the same welcome line under the same toggle. The resub payload has
	// no is_gift field, so the gifted-recipient skip only ever fires on
	// channel.subscribe.
	subAlert := onAlert(
		func(cfg alertsConfig) (bool, string) { return alertOn(cfg.SubEnabled), cfg.SubMessage },
		defaultSubTemplate,
		func(ctx context.Context, ev subscribeEvent) (alertLine, bool) {
			// A gifted recipient is announced through the gift alert on
			// channel.subscription.gift (one line per gifter, not one per
			// recipient), so a gift bomb cannot flood chat with welcome lines.
			if ev.UserLogin == "" || ev.IsGift {
				return alertLine{}, false
			}
			user := chatName(ev.UserName, ev.UserLogin)
			activity.Emit(ctx, ev.BroadcasterUserID, activity.Row{
				Kind: activity.KindEvent,
				Text: user + " subscribed (" + ev.Tier + ")",
				At:   time.Now(),
			})
			return alertLine{ev.BroadcasterUserID, map[string]string{
				"user": user,
				"tier": ev.Tier,
			}}, true
		})
	m.On("channel.subscribe", subAlert)
	m.On("channel.subscription.message", subAlert)

	m.On("channel.subscription.gift", onAlert(
		func(cfg alertsConfig) (bool, string) { return alertOn(cfg.GiftEnabled), cfg.GiftMessage },
		defaultGiftTemplate,
		func(ctx context.Context, ev giftEvent) (alertLine, bool) {
			if ev.BroadcasterUserID == "" || ev.Total <= 0 {
				return alertLine{}, false
			}
			gifter := "An anonymous gifter"
			if !ev.IsAnonymous {
				gifter = displayName(ev.UserName, ev.UserLogin)
			}
			gifter = strings.TrimPrefix(gifter, "@")
			activity.Emit(ctx, ev.BroadcasterUserID, activity.Row{
				Kind: activity.KindEvent,
				Text: gifter + " gifted " + strconv.Itoa(ev.Total) + " subs",
				At:   time.Now(),
			})
			return alertLine{ev.BroadcasterUserID, map[string]string{
				"user":  gifter,
				"count": strconv.Itoa(ev.Total),
				"tier":  ev.Tier,
			}}, true
		}))

	m.On("channel.cheer", onAlert(
		func(cfg alertsConfig) (bool, string) { return alertOn(cfg.CheerEnabled), cfg.CheerMessage },
		defaultCheerTemplate,
		func(_ context.Context, ev cheerEvent) (alertLine, bool) {
			if ev.BroadcasterUserID == "" {
				return alertLine{}, false
			}
			cheerer := "An anonymous cheerer"
			if !ev.IsAnonymous {
				cheerer = displayName(ev.UserName, ev.UserLogin)
			}
			return alertLine{ev.BroadcasterUserID, map[string]string{
				"user": strings.TrimPrefix(cheerer, "@"),
				"bits": strconv.Itoa(ev.Bits),
			}}, true
		}))

	m.On("channel.raid", onAlert(
		func(cfg alertsConfig) (bool, string) { return alertOn(cfg.RaidEnabled), cfg.RaidMessage },
		defaultRaidTemplate,
		func(ctx context.Context, ev raidEvent) (alertLine, bool) {
			if ev.FromBroadcasterUserLogin == "" {
				return alertLine{}, false
			}
			user := chatName(ev.FromBroadcasterUserName, ev.FromBroadcasterUserLogin)
			activity.Emit(ctx, ev.ToBroadcasterUserID, activity.Row{
				Kind: activity.KindEvent,
				Text: user + " raided with " + strconv.Itoa(ev.Viewers) + " viewers",
				At:   time.Now(),
			})
			return alertLine{ev.ToBroadcasterUserID, map[string]string{
				"user":    user,
				"viewers": strconv.Itoa(ev.Viewers),
			}}, true
		}))

	m.On("channel.ad_break.begin", onAlert(
		func(cfg alertsConfig) (bool, string) { return adAlertOn(cfg.AdsEnabled), cfg.AdsMessage },
		defaultAdsTemplate,
		func(_ context.Context, ev adBreakEvent) (alertLine, bool) {
			if ev.BroadcasterUserID == "" {
				return alertLine{}, false
			}
			return alertLine{ev.BroadcasterUserID, map[string]string{
				"duration": strconv.Itoa(ev.DurationSeconds),
			}}, true
		}))

	return m.Build()
}

// followLine renders the follow alert. It is the one render that needs runtime
// deps: the dedupe window is claimed here, inside the render, so it is taken
// only for a channel whose follow alert is actually enabled.
func followLine(cd engine.CooldownStore, log *zap.Logger) func(context.Context, followEvent) (alertLine, bool) {
	return func(ctx context.Context, ev followEvent) (alertLine, bool) {
		if ev.UserLogin == "" || !firstFollowAlert(ctx, cd, log, ev) {
			return alertLine{}, false
		}
		user := chatName(ev.UserName, ev.UserLogin)
		activity.Emit(ctx, ev.BroadcasterUserID, activity.Row{
			Kind: activity.KindEvent,
			Text: user + " followed",
			At:   time.Now(),
		})
		return alertLine{ev.BroadcasterUserID, map[string]string{
			"user": user,
		}}, true
	}
}

// displayName prefers the EventSub display name, falling back to the login
// when Twitch omits it.
func displayName(name, login string) string {
	if name != "" {
		return name
	}
	return login
}

// chatName is displayName as the {user} token: any leading @ is stripped so a
// template can write "@{user}" without doubling it.
func chatName(name, login string) string {
	return strings.TrimPrefix(displayName(name, login), "@")
}
