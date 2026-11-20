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

func (s LiveState) offlineSince(start time.Time, liveAtOpen bool) bool {
	if s.Live {
		return false
	}
	return liveAtOpen || s.ChangedAt.After(start)
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
	if err != nil || !data.round.claimClockNeedsSync(state) {
		return clockReady, err
	}
	applied, err := s.updateClaimClock(ctx, data, state, state.claimClockTime(now))
	if err == nil && !applied {
		return clockConflict, nil
	}
	return clockMoved, err
}

func (s LiveState) claimClockTime(now time.Time) time.Time {
	if s.ChangedAt.IsZero() {
		return now
	}
	return earlier(s.ChangedAt, now)
}

func (r Round) claimClockNeedsSync(state LiveState) bool {
	if !r.ClaimPausedAt.IsZero() {
		return state.Live
	}
	return state.offlineSince(r.SettledAt, r.LiveAtOpen)
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
