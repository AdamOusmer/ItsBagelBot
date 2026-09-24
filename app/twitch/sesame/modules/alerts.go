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

	"go.uber.org/zap"
)

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
	AdsEnabled    string `json:"adsEnabled"`
	AdsMessage    string `json:"adsMessage"`
}

func alertOn(v string) bool { return v != "off" }

func explicitOn(v string) bool { return v == "on" }

const followAlertWindow = 72 * time.Hour

type followEvent struct {
	UserID            string `json:"user_id"`
	UserName          string `json:"user_name"`
	UserLogin         string `json:"user_login"`
	BroadcasterUserID string `json:"broadcaster_user_id"`
}

type alertClaim struct {
	kind          string
	broadcasterID string
	userID        string
	window        time.Duration
}

func (c alertClaim) key() string {
	return "alert:" + c.kind + ":" + c.broadcasterID + ":" + c.userID
}

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

const subAlertWindow = 15 * time.Minute

type subscribeEvent struct {
	UserID            string `json:"user_id"`
	UserName          string `json:"user_name"`
	UserLogin         string `json:"user_login"`
	BroadcasterUserID string `json:"broadcaster_user_id"`
	Tier              string `json:"tier"`
	IsGift            bool   `json:"is_gift"`
}

type giftEvent struct {
	IsAnonymous       bool   `json:"is_anonymous"`
	UserName          string `json:"user_name"`
	UserLogin         string `json:"user_login"`
	BroadcasterUserID string `json:"broadcaster_user_id"`
	Total             int    `json:"total"`
	Tier              string `json:"tier"`
}

type cheerEvent struct {
	IsAnonymous       bool   `json:"is_anonymous"`
	UserName          string `json:"user_name"`
	UserLogin         string `json:"user_login"`
	BroadcasterUserID string `json:"broadcaster_user_id"`
	Bits              int    `json:"bits"`
}

type adBreakEvent struct {
	BroadcasterUserID string `json:"broadcaster_user_id"`
	DurationSeconds   int    `json:"duration_seconds"`
}

type alertLine struct {
	broadcasterID string
	tokens        map[string]string
}

func tokenPairs(m map[string]string) []string {
	out := make([]string, 0, len(m)*2)
	for k, v := range m {
		out = append(out, k, v)
	}
	return out
}

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
		msg := module.KV(tokenPairs(line.tokens)...).WithLocale(module.Locale(c.Locale)).ExpandString(text)
		emit(&module.Output{
			Type:          outgress.TypeChat,
			BroadcasterID: line.broadcasterID,
			Text:          msg,
		})
		return nil
	}
}

func Alerts(d engine.Deps) module.Module {
	m := module.NewModule("alerts", module.KindDefault)

	m.On("channel.follow", onAlert(
		func(cfg alertsConfig) (bool, string) { return alertOn(cfg.FollowEnabled), cfg.FollowMessage },
		"alerts.follow.default",
		claimedLine[followEvent](d.Cooldown, moduleLog(d))))

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

func displayName(name, login string) string {
	if name != "" {
		return name
	}
	return login
}

func chatName(name, login string) string {
	return strings.TrimPrefix(displayName(name, login), "@")
}
