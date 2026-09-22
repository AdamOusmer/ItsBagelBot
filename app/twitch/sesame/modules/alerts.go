// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package modules

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"ItsBagelBot/app/twitch/sesame/engine"
	"ItsBagelBot/app/twitch/sesame/module"
	"ItsBagelBot/internal/activity"
	"ItsBagelBot/internal/domain/i18n"
	"ItsBagelBot/internal/domain/outgress"
	"ItsBagelBot/pkg/codec"
	"ItsBagelBot/pkg/tmpl"

	"go.uber.org/zap"
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
	// alerts above, it fires only on an explicit "on" (see explicitOn), so
	// enabling the module never starts announcing ad breaks by surprise.
	AdsEnabled string `json:"adsEnabled"`
	AdsMessage string `json:"adsMessage"`
}

// alertOn reports whether a sub-alert toggle is on. Only an explicit "off"
// disables it; empty (never set) and "on" both fire, so each alert defaults on.
func alertOn(v string) bool { return v != "off" }

// explicitOn is the inverse posture for opt-in toggles: off until the
// broadcaster explicitly turns it on, so only "on" counts. Used by the ads
// alert, the native shoutout and the lookup modules' linkedOnly toggle, all
// of which must not change behaviour for a channel that never saw the switch.
func explicitOn(v string) bool { return v == "on" }

// followAlertWindow is how long a channel remembers that it already thanked a
// follower. Twitch re-sends channel.follow on every re-follow, so without this
// a viewer can unfollow/refollow in a loop and drive the alert line as fast as
// they can click. Three days is well past the timescale that baiting is fun on,
// and it also swallows the duplicate a JetStream redelivery would otherwise
// turn into a second thank-you.
//
// The window is deliberately per (channel, follower): a genuine follower who
// leaves and comes back later in the week is still thanked, and one channel's
// alerts never suppress another's. See alertClaim.key for the Valkey key, and
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

// alertClaim is the per-channel, per-viewer dedupe window an alert takes
// before it posts. kind names the Valkey key and the warn line. Both ids are
// Twitch's numeric user ids, so a rename never re-opens the window the way a
// login-keyed claim would.
type alertClaim struct {
	kind          string
	broadcasterID string
	userID        string
	window        time.Duration
}

func (c alertClaim) key() string {
	return "alert:" + c.kind + ":" + c.broadcasterID + ":" + c.userID
}

// first claims the window and reports whether the alert should fire. Callers
// run it only once the alert is known to be enabled (and, for subs, past the
// gifted-recipient skip), so a channel with the alert off never burns a
// window it would want later.
//
// It fails open: with no cooldown store wired, no user id on the payload, or
// Valkey unreachable, the alert still posts. A missed thank-you is a worse
// outcome than the duplicate an outage can let through, and the abuse this
// gate exists for cannot cause the outage.
func (c alertClaim) first(ctx context.Context, cd engine.CooldownStore, log *zap.Logger) bool {
	if cd == nil || c.userID == "" {
		return true
	}
	ok, err := cd.Allow(ctx, c.key(), c.window)
	if err != nil {
		log.Warn("alerts: "+c.kind+" cooldown unavailable, alerting anyway",
			zap.String("broadcaster_id", c.broadcasterID),
			zap.String("user_id", c.userID),
			zap.Error(err),
		)
		return true
	}
	return ok
}

// subAlertWindow is how long a channel remembers that it already welcomed a
// subscriber. Twitch docs say channel.subscribe excludes resubs, but in
// production on 2026-09-18 it fired on a 2-month renewal anyway, and the
// viewer's share click then fired channel.subscription.message seconds later,
// posting the same welcome line twice. 15 minutes covers renew-then-share plus
// a JetStream redelivery, without holding the window long enough to matter.
//
// followAlertWindow's 72 hours was rejected here: a share days after the
// renewal is still a legitimate second moment worth welcoming, and a real
// resub next month must never be swallowed by a stale claim.
//
// The window is per (channel, subscriber), same as follow: one channel's
// alerts never suppress another's. See alertClaim.key for the Valkey key.
const subAlertWindow = 15 * time.Minute

// subscribeEvent is the subset of the channel.subscribe and
// channel.subscription.message EventSub payloads we use. IsGift marks a
// gifted recipient: Twitch fires one channel.subscribe per recipient of a
// gift, on top of the single channel.subscription.gift for the gifter.
type subscribeEvent struct {
	UserID            string `json:"user_id"`
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
func onAlert[T any](pick func(alertsConfig) (bool, string), fallbackKey string, render func(context.Context, *module.Context, T) (alertLine, bool)) module.EventHandler {
	return func(ctx context.Context, c *module.Context, emit module.Emit) error {
		var cfg alertsConfig
		_ = c.Decode(&cfg)
		enabled, text := pick(cfg)
		if !enabled || len(c.Env.Event) == 0 {
			return nil
		}
		var ev T
		if err := codec.Unmarshal(c.Env.Event, &ev); err != nil {
			return err
		}
		line, ok := render(ctx, c, ev)
		if !ok {
			return nil
		}
		if text == "" {
			text = i18n.T(c.Locale, fallbackKey)
		}
		msg := module.ExpandString(text, func(tok tmpl.Token) (string, bool) {
			if v, found := line.tokens[tok.Key()]; found {
				return v, true
			}
			return tmpl.Dynamic(tok)
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
// Follow and sub are deduplicated (followAlertWindow, subAlertWindow): follow
// because a viewer can re-trigger it at will, sub because Twitch delivers one
// renewal as channel.subscribe plus channel.subscription.message on share.
// Gifts, cheers and raids each cost the sender something, and ad breaks are
// the channel's own, so those are announced every time.
func Alerts(d engine.Deps) module.Module {
	m := module.NewModule("alerts", module.KindDefault)

	m.On("channel.follow", onAlert(
		func(cfg alertsConfig) (bool, string) { return alertOn(cfg.FollowEnabled), cfg.FollowMessage },
		"alerts.follow.default",
		claimedLine[followEvent](d.Cooldown, moduleLog(d))))

	// Both sub events share one toggle, template and dedupe window
	// (subAlertWindow), so a renewal followed by a share click posts one welcome line.
	subAlert := onAlert(
		func(cfg alertsConfig) (bool, string) { return alertOn(cfg.SubEnabled), cfg.SubMessage },
		"alerts.sub.default",
		claimedLine[subscribeEvent](d.Cooldown, moduleLog(d)))
	m.On("channel.subscribe", subAlert)
	m.On("channel.subscription.message", subAlert)

	m.On("channel.subscription.gift", onAlert(
		func(cfg alertsConfig) (bool, string) { return alertOn(cfg.GiftEnabled), cfg.GiftMessage },
		"alerts.gift.default",
		giftLine))

	m.On("channel.cheer", onAlert(
		func(cfg alertsConfig) (bool, string) { return alertOn(cfg.CheerEnabled), cfg.CheerMessage },
		"alerts.cheer.default",
		func(_ context.Context, _ *module.Context, ev cheerEvent) (alertLine, bool) {
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
		"alerts.raid.default",
		func(ctx context.Context, c *module.Context, ev raidEvent) (alertLine, bool) {
			if ev.FromBroadcasterUserLogin == "" {
				return alertLine{}, false
			}
			user := chatName(ev.FromBroadcasterUserName, ev.FromBroadcasterUserLogin)
			activity.Emit(ctx, ev.ToBroadcasterUserID, activity.Row{
				Kind: activity.KindEvent,
				Text: fmt.Sprintf(i18n.T(c.Locale, "activity.event.raid"), user, ev.Viewers),
				At:   time.Now(),
			})
			return alertLine{ev.ToBroadcasterUserID, map[string]string{
				"user":    user,
				"viewers": strconv.Itoa(ev.Viewers),
			}}, true
		}))

	m.On("channel.ad_break.begin", onAlert(
		func(cfg alertsConfig) (bool, string) { return explicitOn(cfg.AdsEnabled), cfg.AdsMessage },
		"alerts.ads.default",
		func(_ context.Context, _ *module.Context, ev adBreakEvent) (alertLine, bool) {
			if ev.BroadcasterUserID == "" {
				return alertLine{}, false
			}
			return alertLine{ev.BroadcasterUserID, map[string]string{
				"duration": strconv.Itoa(ev.DurationSeconds),
			}}, true
		}))

	return m.Build()
}

// claimedEvent is an alert payload that takes a dedupe window (alertClaim)
// before it posts: follow and sub today. skip reports payloads that never
// alert (no login, or a gifted recipient announced through the gift alert
// instead); it runs before the claim so those never burn a window.
type claimedEvent interface {
	skip() bool
	claim() alertClaim
	activityText(c *module.Context) string
	tokens() map[string]string
}

func (ev followEvent) skip() bool { return ev.UserLogin == "" }
func (ev followEvent) claim() alertClaim {
	return alertClaim{"follow", ev.BroadcasterUserID, ev.UserID, followAlertWindow}
}
func (ev followEvent) user() string { return chatName(ev.UserName, ev.UserLogin) }
func (ev followEvent) activityText(c *module.Context) string {
	return fmt.Sprintf(i18n.T(c.Locale, "activity.event.follow"), ev.user())
}
func (ev followEvent) tokens() map[string]string { return map[string]string{"user": ev.user()} }

// skip also covers a gifted recipient: channel.subscription.gift announces
// those separately, and the resub payload has no is_gift field, so that half
// of skip only ever fires on channel.subscribe.
func (ev subscribeEvent) skip() bool { return ev.UserLogin == "" || ev.IsGift }
func (ev subscribeEvent) claim() alertClaim {
	return alertClaim{"sub", ev.BroadcasterUserID, ev.UserID, subAlertWindow}
}
func (ev subscribeEvent) user() string { return chatName(ev.UserName, ev.UserLogin) }
func (ev subscribeEvent) activityText(c *module.Context) string {
	return fmt.Sprintf(i18n.T(c.Locale, "activity.event.subscribe"), ev.user(), ev.Tier)
}
func (ev subscribeEvent) tokens() map[string]string {
	return map[string]string{"user": ev.user(), "tier": ev.Tier}
}

// claimedLine builds the chat line for a claimedEvent: skip, claim the
// window, record the activity row, hand back the template tokens. The claim
// runs only once the alert is known to be enabled (onAlert checks the toggle
// first), so a channel with the alert off never burns a window it would want
// later.
func claimedLine[E claimedEvent](cd engine.CooldownStore, log *zap.Logger) func(context.Context, *module.Context, E) (alertLine, bool) {
	return func(ctx context.Context, mctx *module.Context, ev E) (alertLine, bool) {
		if ev.skip() {
			return alertLine{}, false
		}
		claim := ev.claim()
		if !claim.first(ctx, cd, log) {
			return alertLine{}, false
		}
		activity.Emit(ctx, claim.broadcasterID, activity.Row{
			Kind: activity.KindEvent,
			Text: ev.activityText(mctx),
			At:   time.Now(),
		})
		return alertLine{claim.broadcasterID, ev.tokens()}, true
	}
}

// giftLine renders the gift alert for channel.subscription.gift: one line
// per gifter, not one per recipient (see subscribeEvent.skip).
func giftLine(ctx context.Context, c *module.Context, ev giftEvent) (alertLine, bool) {
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
		Text: fmt.Sprintf(i18n.T(c.Locale, "activity.event.gift"), gifter, ev.Total),
		At:   time.Now(),
	})
	return alertLine{ev.BroadcasterUserID, map[string]string{
		"user":  gifter,
		"count": strconv.Itoa(ev.Total),
		"tier":  ev.Tier,
	}}, true
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
