// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package draw

import (
	"context"
	"encoding/json"
	"strconv"
	"time"

	"ItsBagelBot/pkg/valkey"
)

type settler struct {
	defaultWinners int
	validate       func(Terms) error
	announceOn     SlotState
	afterWin       slotStep
}

var settlers = map[Kind]settler{
	KindGiveaway: {Pins.GiveawayDefaultWinners, validateGiveaway, SlotWon, expireWin},
	KindRaffle:   {Pins.RaffleDefaultWinners, validateRaffle, SlotPaid, payWin},
}

func validateGiveaway(t Terms) error {
	if t.Prize.Points != 0 || t.Winners.Auto {
		return ErrInvalidTerms
	}
	return nil
}

func validateRaffle(t Terms) error {
	if !pointPrize(t.Prize) || t.ClaimWindow != 0 {
		return ErrInvalidTerms
	}
	return nil
}

func pointPrize(p Prize) bool { return p.Item == "" && p.Points > 0 }

type roundStep func(context.Context, *Store, snapshot, time.Time) error

func phaseSteps() map[Phase]roundStep {
	return map[Phase]roundStep{
		PhaseOpen: closeDue, PhaseClosing: seedSlots, PhaseSettling: settleSlots,
		PhaseDone: teardownRound, PhaseCancelled: teardownRound,
	}
}

func closeDue(ctx context.Context, s *Store, data snapshot, now time.Time) error {
	r := data.round
	if now.Before(r.ClosesAt) {
		return s.schedule(ctx, data, r.ClosesAt.Sub(now))
	}
	shift := phaseShift(r, PhaseClosing, now)
	shift.Match = timeField("closes_at", r.ClosesAt)
	_, err := keysFor(r.Channel, r.ID).Shift(ctx, s.client, shift)
	return err
}

func seedSlots(ctx context.Context, s *Store, data snapshot, now time.Time) error {
	r := data.round
	policy := r.Terms.Winners
	if err := decodeOverride(data, &policy); err != nil {
		return err
	}
	count := len(data.stored.Entries)
	r.Winners = min(count, ResolveWinners(policy, count, r.Terms.Prize))
	r.SettledAt = now
	shift, err := seedShift(r)
	if err != nil {
		return err
	}
	ok, err := keysFor(r.Channel, r.ID).Shift(ctx, s.client, shift)
	if ok && r.Winners == 0 {
		s.cfg.Announcer.Announce(ctx, Notice{Event: NoticeEmpty, Round: r})
	}
	return err
}

func decodeOverride(data snapshot, p *WinnerPolicy) error {
	raw := data.stored.Round["winner_override"]
	if raw == "" {
		return nil
	}
	return json.Unmarshal([]byte(raw), p)
}

func seedShift(r Round) (valkey.PhaseShift, error) {
	if r.Winners == 0 {
		return phaseShift(r, PhaseDone, r.SettledAt), nil
	}
	shift := phaseShift(r, PhaseSettling, r.SettledAt)
	shift.Fields = []valkey.FieldValue{{Field: "winners", Value: strconv.Itoa(r.Winners)}, timeField("settled_at", r.SettledAt)}
	for n := 1; n <= r.Winners; n++ {
		raw, err := json.Marshal(Slot{N: n, Generation: 1, State: SlotPending})
		if err != nil {
			return valkey.PhaseShift{}, err
		}
		shift.Seed = append(shift.Seed, valkey.FieldValue{Field: strconv.Itoa(n), Value: string(raw)})
	}
	return shift, nil
}

func settleSlots(ctx context.Context, s *Store, data snapshot, now time.Time) error {
	if allTerminal(data) {
		return s.finish(ctx, data, now)
	}
	if verifyingNoticeDue(data, now) && hasWorkingSlots(data) {
		return s.markVerifying(ctx, data.round, now)
	}
	return s.advanceSlots(ctx, data, now)
}

func verifyingNoticeDue(data snapshot, now time.Time) bool {
	return data.stored.Round["verifying_notice"] == "" && !now.Before(data.round.SettledAt.Add(Pins.VerifyingNotice))
}

func allTerminal(data snapshot) bool {
	for _, slot := range data.slots {
		if !slotTerminal(data.round, slot) {
			return false
		}
	}
	return true
}

func hasWorkingSlots(data snapshot) bool {
	for _, slot := range data.slots {
		if slotWorking(data.round, slot) {
			return true
		}
	}
	return false
}

func slotWorking(r Round, slot Slot) bool {
	switch slot.State {
	case SlotPending, SlotVerifying:
		return true
	case SlotWon:
		return r.Terms.Kind == KindRaffle
	default:
		return false
	}
}

func slotTerminal(r Round, slot Slot) bool {
	switch slot.State {
	case SlotClaimed, SlotPaid, SlotUnpaid, SlotUnfilled, SlotEnded:
		return true
	case SlotWon:
		return r.Terms.Kind == KindGiveaway && r.Terms.ClaimWindow == 0
	default:
		return false
	}
}

func (s *Store) markVerifying(ctx context.Context, r Round, now time.Time) error {
	shift := phaseShift(r, PhaseSettling, now)
	shift.Match = valkey.FieldValue{Field: "verifying_notice"}
	shift.Fields = []valkey.FieldValue{{Field: "verifying_notice", Value: "1"}}
	ok, err := keysFor(r.Channel, r.ID).Shift(ctx, s.client, shift)
	if ok {
		s.cfg.Announcer.Announce(ctx, Notice{Event: NoticeVerifying, Round: r})
	}
	return err
}

func (s *Store) finish(ctx context.Context, data snapshot, now time.Time) error {
	r := data.round
	if r.Terms.Kind == KindGiveaway {
		if err := s.cfg.Winners.Record(ctx, r, data.slots); err != nil {
			return err
		}
	}
	_, err := keysFor(r.Channel, r.ID).Shift(ctx, s.client, phaseShift(r, PhaseDone, now))
	return err
}

func teardownRound(ctx context.Context, s *Store, data snapshot, _ time.Time) error {
	r := data.round
	return keysFor(r.Channel, r.ID).Teardown(ctx, s.client, string(r.ID))
}
