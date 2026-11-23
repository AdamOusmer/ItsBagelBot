// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"time"

	"ItsBagelBot/app/twitch/sesame/engine/draw"
	"ItsBagelBot/app/twitch/sesame/module"
	"ItsBagelBot/internal/analytics"
	"ItsBagelBot/internal/domain/i18n"

	"go.uber.org/zap"
)

type drawAnnouncer struct {
	cfg       AnnouncementConfig
	log       *zap.Logger
	analytics *analytics.Recorder
}

func NewDrawAnnouncer(cfg AnnouncementConfig, log *zap.Logger, rec *analytics.Recorder) draw.Announcer {
	return drawAnnouncer{cfg: cfg, log: log, analytics: rec}
}

type noticeRenderer func(drawAnnouncer, context.Context, string, draw.Notice) []string

var noticeRenderers = map[draw.Kind]noticeRenderer{}

func (a drawAnnouncer) Announce(ctx context.Context, notice draw.Notice) error {
	if notice.Round.Terms.Kind == draw.KindRaffle {
		a.recordRaffleNotice(ctx, notice)
	}
	if notice.Event == draw.NoticeDrawn {
		return nil
	}
	channel := uint64(notice.Round.Channel)
	user, _ := a.cfg.Proj.User(ctx, channel)
	render := noticeRenderers[notice.Round.Terms.Kind]
	if render == nil {
		render = fallbackDrawNotice
	}
	var postErr error
	for _, line := range render(a, ctx, user.Locale, notice) {
		delivery := announcementLine{text: line, confirmed: notice.Event == draw.NoticeRefusals}
		postErr = errors.Join(postErr, a.cfg.post(ctx, channel, delivery, a.log))
	}
	return postErr
}

func fallbackDrawNotice(a drawAnnouncer, ctx context.Context, locale string, notice draw.Notice) []string {
	if notice.Event == draw.NoticeRemind {
		return []string{drawReminder(locale, notice)}
	}
	currency := DrawCurrencyName(ctx, a.cfg.Proj, uint64(notice.Round.Channel))
	return []string{module.KV("targets", DrawTargets(notice.Slots), "points", currency, "amount", drawPoints(notice.Round)).WithNamespace("draw").
		ExpandString(i18n.T(locale, drawNoticeKey(notice)))}
}

func drawReminder(locale string, notice draw.Notice) string {
	minutes := (notice.TimeLeft + time.Minute - 1) / time.Minute
	return module.KV("mins", strconv.FormatInt(int64(minutes), 10), "count", strconv.Itoa(notice.Entries)).
		WithNamespace("draw").ExpandString(i18n.T(locale, "draw.remind"))
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
