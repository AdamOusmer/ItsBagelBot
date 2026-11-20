// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package draw

import (
	"context"
	"time"

	"ItsBagelBot/pkg/valkey"
)

func (s *Store) liveState(ctx context.Context, round Round) (LiveState, error) {
	if s.cfg.Live == nil || round.Terms.Kind != KindGiveaway {
		return LiveState{Live: true}, nil
	}
	return s.cfg.Live.DrawLiveState(ctx, uint64(round.Channel))
}

type claimClockResult uint8

const (
	clockReady claimClockResult = iota
	clockMoved
	clockConflict
)

func (s *Store) syncClaimClock(ctx context.Context, data snapshot, now time.Time) (claimClockResult, error) {
	if data.round.Terms.Kind != KindGiveaway || data.round.Terms.ClaimWindow == 0 {
		return clockReady, nil
	}
	state, err := s.liveState(ctx, data.round)
	if err != nil || state.Live == data.round.ClaimPausedAt.IsZero() {
		return clockReady, err
	}
	at := state.ChangedAt
	if at.IsZero() || at.After(now) {
		at = now
	}
	applied, err := s.updateClaimClock(ctx, data, state, at)
	if err == nil && !applied {
		return clockConflict, nil
	}
	return clockMoved, err
}

func (s *Store) updateClaimClock(ctx context.Context, data snapshot, state LiveState, at time.Time) (bool, error) {
	r := data.round
	batch := slotBatch{now: at}
	if state.Live {
		batch.changes = resumedClaims(data.slots, r.ClaimPausedAt, at)
		batch.fields = []valkey.FieldValue{{Field: "claim_paused_at"}}
	} else {
		batch.fields = []valkey.FieldValue{timeField("claim_paused_at", later(at, r.SettledAt))}
	}
	return s.commitSlots(ctx, data, batch)
}

func resumedClaims(slots []Slot, paused, resumed time.Time) []slotChange {
	var changes []slotChange
	for _, slot := range slots {
		if slot.State != SlotWon || slot.Deadline.IsZero() {
			continue
		}
		start := later(paused, fromMillis(slot.WonAtMS))
		next := slot
		next.Deadline = slot.Deadline.Add(max(resumed.Sub(start), 0))
		changes = append(changes, slotChange{prior: slot, next: next})
	}
	return changes
}

func later(a, b time.Time) time.Time {
	if a.After(b) {
		return a
	}
	return b
}

func (r Round) ClaimTime(now time.Time, slot Slot) time.Time {
	if r.ClaimPausedAt.IsZero() {
		return now
	}
	return later(r.ClaimPausedAt, fromMillis(slot.WonAtMS))
}
