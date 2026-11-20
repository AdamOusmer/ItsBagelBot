// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"context"
	"strconv"
	"time"

	"ItsBagelBot/app/twitch/sesame/engine/draw"
	"ItsBagelBot/app/twitch/sesame/module"
	"ItsBagelBot/internal/domain/i18n"
	modulesrpc "ItsBagelBot/internal/domain/rpc/modules"

	"go.uber.org/zap"
)

func init() {
	noticeRenderers[draw.KindGiveaway] = noticeGiveaway
}

func (o GiveawayStartOutcome) OpenedLine(locale string) string {
	if !o.Config.OpenedEnabled {
		return ""
	}
	r := o.Round
	left := r.ClosesAt.Sub(r.OpenedAt)
	requirements := GiveawayRequirements(locale, o.Rules)
	var key giveawayMessageKey = "giveaway.opened"
	if requirements != "" {
		key = "giveaway.opened.requirements"
	}
	return renderGiveaway(module.Locale(locale), o.Config.OpenedMessage, key, module.KV(
		"prize", r.Terms.Prize.Item, "count", strconv.Itoa(r.Terms.Winners.Count),
		"mins", strconv.FormatInt(int64((left+time.Minute-1)/time.Minute), 10),
		"left", i18n.CompactDuration(locale, left), "requirements", requirements))
}

func noticeGiveaway(a drawAnnouncer, ctx context.Context, locale string, notice draw.Notice) []string {
	switch notice.Event {
	case draw.NoticeRefusals:
		return a.giveawayRefusalLines(ctx, module.Locale(locale), notice)
	case draw.NoticeWinners, draw.NoticeNoShow:
		return a.giveawaySlotNotice(ctx, module.Locale(locale), notice)
	case draw.NoticeEmpty, draw.NoticeUnfilled, draw.NoticeVerifying:
		return []string{i18n.T(locale, "giveaway."+string(notice.Event))}
	default:
		return nil
	}
}

func (a drawAnnouncer) giveawaySlotNotice(ctx context.Context, locale module.Locale, notice draw.Notice) []string {
	channel := uint64(notice.Round.Channel)
	settings, err := ReadGiveawayConfig(ctx, a.cfg.Proj, channel)
	if err != nil {
		a.log.Warn("giveaway: failed to read announcement settings", module.BIDField(channel), zap.Error(err))
		return nil
	}
	return giveawaySlotLines(locale, settings, notice)
}

func giveawaySlotLines(locale module.Locale, cfg GiveawayModuleConfig, notice draw.Notice) []string {
	winner := notice.Event == draw.NoticeWinners
	if !cfg.slotNoticeEnabled(winner) {
		return nil
	}
	var key giveawayMessageKey = "giveaway.no_show"
	override := ""
	if winner {
		key, override = giveawayWinKey(notice.Round), cfg.WonMessage
	}
	return chunkMentions(notice.Slots, func(slot draw.Slot) string { return slot.Login }, func(targets string) string {
		return renderGiveaway(locale, override, key, module.KV(
			"targets", targets, "prize", notice.Round.Terms.Prize.Item, "count", strconv.Itoa(len(notice.Slots)),
			"claim", i18n.CompactDuration(string(locale), notice.Round.Terms.ClaimWindow)))
	})
}

func (cfg GiveawayModuleConfig) slotNoticeEnabled(winner bool) bool {
	if winner {
		return bool(cfg.WonEnabled)
	}
	return bool(cfg.NoShowEnabled)
}

func giveawayWinKey(r draw.Round) giveawayMessageKey {
	if r.Terms.ClaimWindow > 0 {
		return "giveaway.won.claim"
	}
	return "giveaway.won"
}

type giveawayMessageKey string

func renderGiveaway(locale module.Locale, override string, key giveawayMessageKey, values module.Palette) string {
	if override == "" {
		override = i18n.T(string(locale), string(key))
	}
	return values.WithNamespace("giveaway").ExpandString(override)
}

func GiveawayHistoryLines(locale string, rows []modulesrpc.GiveawayWinner) []string {
	return chunkMentions(rows, func(row modulesrpc.GiveawayWinner) string { return row.Login }, func(targets string) string {
		return renderGiveaway(module.Locale(locale), "", "giveaway.last", module.KV("targets", targets, "prize", rows[0].Prize))
	})
}
