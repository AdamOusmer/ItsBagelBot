// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"context"
	"slices"
	"strconv"
	"time"

	"ItsBagelBot/app/twitch/sesame/engine/draw"
	"ItsBagelBot/app/twitch/sesame/module"
	"ItsBagelBot/internal/domain/i18n"
	modulesrpc "ItsBagelBot/internal/domain/rpc/modules"
	"ItsBagelBot/pkg/tmpl"

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
	return renderGiveaway(module.Locale(locale), o.openedTemplate(), key, module.KV(
		"prize", r.Terms.Prize.Item, "count", strconv.Itoa(r.Terms.Winners.Count),
		"mins", strconv.FormatInt(int64((left+time.Minute-1)/time.Minute), 10),
		"left", i18n.CompactDuration(locale, left), "requirements", requirements))
}

func (o GiveawayStartOutcome) openedTemplate() string {
	override := o.Config.OpenedMessage
	if !o.windowClamped || override == "" {
		return override
	}
	tokens := tmpl.Lex(override)
	if containsGiveawayField(tokens, "left") && !containsGiveawayField(tokens, "mins") {
		return override
	}
	return ""
}

func containsGiveawayField(tokens []tmpl.Token, field string) bool {
	return slices.ContainsFunc(tokens, func(token tmpl.Token) bool {
		return module.TokenKey(token, "giveaway") == field
	})
}

func noticeGiveaway(a drawAnnouncer, ctx context.Context, locale string, notice draw.Notice) []string {
	switch notice.Event {
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
			"claim", i18n.CompactDuration(string(locale), notice.Round.Terms.ClaimWindow)))
	})
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
