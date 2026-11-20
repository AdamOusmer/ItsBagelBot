// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package modules

import (
	"context"
	"slices"
	"strconv"
	"time"

	"ItsBagelBot/app/twitch/sesame/engine/draw"
	"ItsBagelBot/app/twitch/sesame/module"
	"ItsBagelBot/internal/domain/i18n"
)

type giveawayActionResult struct {
	applied bool
	err     error
	success replyKey
	reroll  bool
	target  draw.SlotAction
}

func (g giveawayCommands) actionReply(ctx context.Context, emit module.Emit, result giveawayActionResult) error {
	switch {
	case result.err != nil:
		return result.err
	case !result.applied:
		return g.explainAction(ctx, emit, result)
	case result.success != "":
		g.reply(emit, "", result.success)
	}
	return nil
}

func (g giveawayCommands) explainAction(ctx context.Context, emit module.Emit, result giveawayActionResult) error {
	rounds, err := g.deps.Draw.Live(ctx, g.action().Channel)
	if err != nil {
		return err
	}
	settling := findSettlingGiveaway(rounds)
	if result.reroll && settling.ID != "" {
		g.explainRoundAction(emit, settling, result)
		return nil
	}
	for _, view := range rounds {
		if relevantGiveaway(view, result) {
			g.explainRoundAction(emit, view, result)
			return nil
		}
	}
	g.reply(emit, "", "giveaway.status.closed")
	return nil
}

func findSettlingGiveaway(rounds []draw.RoundView) draw.RoundView {
	i := slices.IndexFunc(rounds, func(view draw.RoundView) bool {
		return view.Terms.Kind == draw.KindGiveaway && view.Phase == draw.PhaseSettling
	})
	if i < 0 {
		return draw.RoundView{}
	}
	return rounds[i]
}

func relevantGiveaway(view draw.RoundView, result giveawayActionResult) bool {
	if view.Terms.Kind != draw.KindGiveaway {
		return false
	}
	if result.reroll {
		return view.Phase == draw.PhaseClosing || view.Phase == draw.PhaseOpen
	}
	return view.Phase == draw.PhaseClosing || view.Phase == draw.PhaseSettling
}

func (g giveawayCommands) explainRoundAction(emit module.Emit, view draw.RoundView, result giveawayActionResult) {
	if !result.reroll || view.Phase == draw.PhaseClosing {
		g.reply(emit, "", "giveaway.action.settling")
		return
	}
	left := remainingClaim(view, result.target, time.Now())
	if left > 0 {
		g.reply(emit, "", "giveaway.action.claim_pending",
			"left", strconv.FormatInt(int64((left+time.Second-1)/time.Second), 10)+" "+i18n.T(g.c.Locale, "time.short_second"))
		return
	}
	g.reply(emit, "", "giveaway.action.no_reroll")
}

func remainingClaim(view draw.RoundView, target draw.SlotAction, now time.Time) time.Duration {
	var left time.Duration
	for _, slot := range view.Slots {
		if slot.State != draw.SlotWon || !target.Matches(slot) {
			continue
		}
		remaining := slot.Deadline.Sub(view.ClaimTime(now, slot))
		if remaining > 0 && (left == 0 || remaining < left) {
			left = remaining
		}
	}
	return left
}
