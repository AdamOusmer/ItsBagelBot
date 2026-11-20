// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"context"
	"strconv"
	"strings"
	"time"

	"ItsBagelBot/app/twitch/sesame/engine/draw"
	"ItsBagelBot/app/twitch/sesame/module"
	"ItsBagelBot/internal/domain/i18n"

	"go.uber.org/zap"
)

type drawAnnouncer struct {
	cfg AnnouncementConfig
	log *zap.Logger
}

func NewDrawAnnouncer(cfg AnnouncementConfig, log *zap.Logger) draw.Announcer {
	return drawAnnouncer{cfg: cfg, log: log}
}

type noticeRenderer func(context.Context, AnnouncementConfig, string, draw.Notice) string

var noticeRenderers = map[draw.Kind]noticeRenderer{}

func (a drawAnnouncer) Announce(ctx context.Context, notice draw.Notice) {
	channel := uint64(notice.Round.Channel)
	user, _ := a.cfg.Proj.User(ctx, channel)
	render := noticeRenderers[notice.Round.Terms.Kind]
	if render == nil {
		render = fallbackDrawNotice
	}
	a.cfg.Post(ctx, channel, render(ctx, a.cfg, user.Locale, notice), a.log)
}

func fallbackDrawNotice(ctx context.Context, cfg AnnouncementConfig, locale string, notice draw.Notice) string {
	if notice.Event == draw.NoticeRemind {
		return drawReminder(ctx, cfg, locale, notice)
	}
	return module.KV("targets", DrawTargets(notice.Slots), "points", drawPoints(notice.Round)).WithNamespace("draw").
		ExpandString(i18n.T(locale, drawNoticeKey(notice)))
}

func drawReminder(ctx context.Context, cfg AnnouncementConfig, locale string, notice draw.Notice) string {
	if notice.Round.Terms.Kind == draw.KindRaffle {
		return raffleDrawReminder(ctx, cfg, locale, notice)
	}
	minutes := (notice.TimeLeft + time.Minute - 1) / time.Minute
	return module.KV("mins", strconv.FormatInt(int64(minutes), 10), "count", strconv.Itoa(notice.Entries)).
		WithNamespace("draw").ExpandString(i18n.T(locale, "draw.remind"))
}

func raffleDrawReminder(ctx context.Context, cfg AnnouncementConfig, locale string, notice draw.Notice) string {
	if !ReadRaffleConfig(ctx, cfg.Proj, uint64(notice.Round.Channel)).RemindOn() {
		return ""
	}
	a := RaffleAnnouncement{Locale: locale}
	left := (notice.TimeLeft + time.Second - 1) / time.Second
	return a.RemindText(int64(left), int64(notice.Entries))
}

func DrawTargets(slots []draw.Slot) string {
	names := make([]string, 0, len(slots))
	for _, slot := range slots {
		if slot.Login != "" {
			names = append(names, "@"+slot.Login)
		}
	}
	return strings.Join(names, ", ")
}

func drawNoticeKey(n draw.Notice) string {
	switch {
	case n.Event != draw.NoticeWinners:
		return "draw." + string(n.Event)
	case n.Round.Terms.Kind == draw.KindRaffle:
		return "draw.winners.raffle"
	case n.Round.Terms.ClaimWindow > 0:
		return "draw.winners.claim"
	default:
		return "draw.winners"
	}
}

func drawPoints(r draw.Round) string {
	if r.Winners == 0 {
		return ""
	}
	return strconv.FormatInt(r.Terms.Prize.Points/int64(r.Winners), 10)
}
