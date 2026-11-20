// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"cmp"
	"context"
	"slices"
	"strconv"
	"time"

	"ItsBagelBot/app/twitch/sesame/engine/draw"
	"ItsBagelBot/app/twitch/sesame/engine/eligibility"
	"ItsBagelBot/app/twitch/sesame/module"
	"ItsBagelBot/internal/domain/i18n"
)

func (a drawAnnouncer) giveawayRefusalLines(ctx context.Context, locale module.Locale, notice draw.Notice) []string {
	currency := DrawCurrencyName(ctx, a.cfg.Proj, uint64(notice.Round.Channel))
	grouped := map[eligibility.Reason][]draw.RefusedViewer{}
	for _, viewer := range notice.Refused {
		reason := eligibility.Reason(viewer.Reason)
		grouped[reason] = append(grouped[reason], viewer)
	}
	var lines []string
	for _, reason := range []eligibility.Reason{eligibility.ReasonBroadcaster, eligibility.ReasonMod, eligibility.ReasonRole, eligibility.ReasonPoints, eligibility.ReasonWatch} {
		if viewers := grouped[reason]; len(viewers) != 0 {
			lines = append(lines, renderRefusedGroup(locale, viewers, currency)...)
		}
	}
	return lines
}

func renderRefusedGroup(locale module.Locale, viewers []draw.RefusedViewer, currency string) []string {
	header := viewers[0]
	need := strconv.FormatInt(header.Need, 10)
	if header.Reason == string(eligibility.ReasonWatch) {
		need = watchRefusalTime(locale, time.Duration(header.Need)*time.Second)
	}
	return chunkJoinedMentions(sortedRefusedViewers(viewers), mentionFormat[draw.RefusedViewer]{
		login:     func(viewer draw.RefusedViewer) string { return viewer.Login },
		separator: " ",
		render: func(targets string) string {
			return module.KV("targets", targets, "need", need, "points", currency).WithNamespace("eligibility").
				ExpandString(i18n.T(string(locale), "eligibility.batch."+header.Reason))
		},
	})
}

func sortedRefusedViewers(viewers []draw.RefusedViewer) []draw.RefusedViewer {
	slices.SortFunc(viewers, func(a, b draw.RefusedViewer) int { return cmp.Compare(a.Login, b.Login) })
	return viewers
}

func watchRefusalTime(locale module.Locale, d time.Duration) string {
	if d < time.Minute || d%time.Minute != 0 {
		return i18n.CompactDuration(string(locale), d)
	}
	return i18n.HumanizeDuration(string(locale), d)
}
