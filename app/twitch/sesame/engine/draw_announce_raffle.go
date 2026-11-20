// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"context"
	"strconv"
	"time"

	"ItsBagelBot/app/twitch/sesame/engine/draw"
	catalog "ItsBagelBot/internal/domain/analytics"
	"ItsBagelBot/pkg/tmpl"
)

func init() {
	noticeRenderers[draw.KindRaffle] = noticeRaffle
}

func noticeRaffle(announcer drawAnnouncer, ctx context.Context, locale string, notice draw.Notice) []string {
	cfg := announcer.cfg
	a := RaffleAnnouncement{Locale: locale}
	switch notice.Event {
	case draw.NoticeWinners:
		return raffleWinnerLines(ctx, cfg, a, notice)
	case draw.NoticeUnpaid:
		loyalty, _ := ReadLoyaltyConfig(ctx, cfg.Proj, uint64(notice.Round.Channel))
		return []string{a.render("", "raffle.payout.failed", "points", loyalty.Name())}
	case draw.NoticeEmpty:
		return []string{a.render("", "raffle.draw.empty")}
	case draw.NoticeVerifying:
		return []string{a.render("", "raffle.verifying")}
	case draw.NoticeRemind:
		return raffleReminder(ctx, cfg, a, notice)
	default:
		return nil
	}
}

func raffleWinnerLines(ctx context.Context, cfg AnnouncementConfig, a RaffleAnnouncement, notice draw.Notice) []string {
	channel := uint64(notice.Round.Channel)
	raffle := ReadRaffleConfig(ctx, cfg.Proj, channel)
	if !raffle.WonOn() {
		return nil
	}
	loyalty, _ := ReadLoyaltyConfig(ctx, cfg.Proj, channel)
	override := pointsRaffleTemplate(raffle.WonMessage)
	key := "raffle.won.paid"
	if len(notice.Slots) == 1 {
		key = "raffle.won.paid.single"
	}
	return chunkMentions(notice.Slots, func(slot draw.Slot) string { return slot.Login }, func(targets string) string {
		return a.render(override, key,
			"targets", targets, "count", strconv.Itoa(len(notice.Slots)),
			"entrants", strconv.Itoa(notice.Entries), "amount", drawPoints(notice.Round), "points", loyalty.Name())
	})
}

func pointsRaffleTemplate(text string) string {
	for _, tok := range tmpl.WithCondRefs(tmpl.Lex(text)) {
		switch tmpl.TokenKey(tok, "raffle") {
		case "claim", "prize", "won:claim", "won:prize":
			return ""
		}
	}
	return text
}

func raffleReminder(ctx context.Context, cfg AnnouncementConfig, a RaffleAnnouncement, notice draw.Notice) []string {
	if !ReadRaffleConfig(ctx, cfg.Proj, uint64(notice.Round.Channel)).RemindOn() {
		return nil
	}
	left := (notice.TimeLeft + time.Second - 1) / time.Second
	return []string{a.RemindText(int64(left), int64(notice.Entries))}
}

func (a drawAnnouncer) recordRaffleNotice(ctx context.Context, notice draw.Notice) {
	game := RaffleGame(notice.Round)
	switch notice.Event {
	case draw.NoticeDrawn:
		a.analytics.AddContext(ctx, catalog.GamesRounds, 1, game, "drawn")
		recordEntrants(a.analytics, game, int64(notice.Entries))
	case draw.NoticeEmpty:
		a.analytics.AddContext(ctx, catalog.GamesRounds, 1, game, "empty")
	case draw.NoticeWinners:
		a.analytics.AddContext(ctx, catalog.PointsFlows, int64(len(notice.Slots)), "raffle_payout")
	}
}
