// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"context"
	"strconv"
	"strings"

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

func (a drawAnnouncer) Announce(ctx context.Context, notice draw.Notice) {
	channel := uint64(notice.Round.Channel)
	user, _ := a.cfg.Proj.User(ctx, channel)
	line := module.KV("targets", DrawTargets(notice.Slots), "points", drawPoints(notice.Round)).WithNamespace("draw").
		ExpandString(i18n.T(user.Locale, drawNoticeKey(notice)))
	a.cfg.Post(ctx, channel, line, a.log)
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
