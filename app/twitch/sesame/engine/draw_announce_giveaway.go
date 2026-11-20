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
	return renderGiveaway(locale, o.Config.OpenedMessage, key, module.KV(
		"prize", r.Terms.Prize.Item, "count", strconv.Itoa(r.Terms.Winners.Count),
		"mins", strconv.FormatInt(int64((left+time.Minute-1)/time.Minute), 10),
		"left", i18n.CompactDuration(locale, left), "requirements", requirements))
}

func noticeGiveaway(ctx context.Context, cfg AnnouncementConfig, locale string, notice draw.Notice) []string {
	switch notice.Event {
	case draw.NoticeWinners, draw.NoticeNoShow:
		settings, err := ReadGiveawayConfig(ctx, cfg.Proj, uint64(notice.Round.Channel))
		if err != nil {
			settings = defaultGiveawayConfig()
		}
		return giveawaySlotLines(locale, settings, notice)
	case draw.NoticeEmpty, draw.NoticeUnfilled, draw.NoticeVerifying:
		return []string{i18n.T(locale, "giveaway."+string(notice.Event))}
	default:
		return nil
	}
}

func giveawaySlotLines(locale string, cfg GiveawayModuleConfig, notice draw.Notice) []string {
	var key giveawayMessageKey = "giveaway.no_show"
	override := ""
	winner := notice.Event == draw.NoticeWinners
	switch {
	case winner && !cfg.WonEnabled:
		return nil
	case !winner && !cfg.NoShowEnabled:
		return nil
	case winner:
		key, override = giveawayWinKey(notice.Round), cfg.WonMessage
	}
	return chunkMentions(notice.Slots, func(slot draw.Slot) string { return slot.Login }, func(targets string) string {
		return renderGiveaway(locale, override, key, module.KV(
			"targets", targets, "prize", notice.Round.Terms.Prize.Item, "count", strconv.Itoa(len(notice.Slots)),
			"claim", i18n.CompactDuration(locale, notice.Round.Terms.ClaimWindow)))
	})
}

func giveawayWinKey(r draw.Round) giveawayMessageKey {
	if r.Terms.ClaimWindow > 0 {
		return "giveaway.won.claim"
	}
	return "giveaway.won"
}

type giveawayMessageKey string

func renderGiveaway(locale, override string, key giveawayMessageKey, values module.Palette) string {
	if override == "" {
		override = i18n.T(locale, string(key))
	}
	return values.WithNamespace("giveaway").ExpandString(override)
}

func GiveawayHistoryLines(locale string, rows []modulesrpc.GiveawayWinner) []string {
	return chunkMentions(rows, func(row modulesrpc.GiveawayWinner) string { return row.Login }, func(targets string) string {
		return renderGiveaway(locale, "", "giveaway.last", module.KV("targets", targets, "prize", rows[0].Prize))
	})
}
