// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"context"
	"time"

	"ItsBagelBot/app/twitch/sesame/engine/draw"
	"ItsBagelBot/app/twitch/sesame/engine/eligibility"
	drawrpc "ItsBagelBot/internal/domain/rpc/draw"
)

func (s GiveawayRPC) live(ctx context.Context, in giveawayRPCInput) (drawrpc.GiveawayReply, error) {
	views, err := s.Deps.Draw.Live(ctx, in.channel)
	if err != nil {
		return drawrpc.GiveawayReply{}, err
	}
	rounds, err := giveawayLiveRounds(s.locale(ctx, in.channel), views)
	return drawrpc.GiveawayReply{Rounds: &rounds}, err
}

func (s GiveawayRPC) locale(ctx context.Context, channel draw.ChannelID) string {
	user, _ := s.Deps.Proj.User(ctx, uint64(channel))
	return user.Locale
}

func giveawayLiveRounds(locale string, views []draw.RoundView) ([]drawrpc.GiveawayRound, error) {
	rounds := make([]drawrpc.GiveawayRound, 0, len(views))
	for _, view := range views {
		if view.Terms.Kind != draw.KindGiveaway {
			continue
		}
		rules, err := eligibility.Parse(view.Terms.Rules)
		if err != nil {
			return nil, err
		}
		rounds = append(rounds, giveawayRoundWire(view, giveawayRequirementLines(locale, rules)))
	}
	return rounds, nil
}

func giveawayRoundWire(view draw.RoundView, requirements []string) drawrpc.GiveawayRound {
	slots := make([]drawrpc.GiveawaySlot, 0, len(view.Slots))
	now := time.Now()
	for _, slot := range view.Slots {
		slots = append(slots, giveawaySlotWire(slot, now))
	}
	return drawrpc.GiveawayRound{ID: string(view.ID), Kind: string(view.Terms.Kind), Phase: string(view.Phase),
		Prize: view.Terms.Prize.Item, Winners: view.Terms.Winners.Count, OpenedMS: view.OpenedAt.UnixMilli(),
		ClosesMS: view.ClosesAt.UnixMilli(), Entries: view.Entries,
		Refused: view.Refused, ClaimPausedAtMS: giveawayClaimPause(view.Round), Slots: slots, Requirements: requirements}
}

func giveawaySlotWire(slot draw.Slot, now time.Time) drawrpc.GiveawaySlot {
	row := drawrpc.GiveawaySlot{N: slot.N, Generation: slot.Generation, ViewerID: string(slot.Viewer),
		Login: slot.Login, State: string(slot.State), CanSkipRule: draw.CanSkipRule(slot, now)}
	if !slot.Deadline.IsZero() {
		row.DeadlineMS = slot.Deadline.UnixMilli()
	}
	for _, prior := range slot.History {
		if prior.Reason != "" {
			row.Rejected = append(row.Rejected, drawrpc.GiveawayCandidate{
				ViewerID: string(prior.Viewer), Login: prior.Login, Generation: prior.Generation,
				Reason: prior.Reason, Detail: prior.Detail,
			})
		}
	}
	return row
}

func giveawayClaimPause(round draw.Round) int64 {
	if round.ClaimPausedAt.IsZero() {
		return 0
	}
	return round.ClaimPausedAt.UnixMilli()
}
