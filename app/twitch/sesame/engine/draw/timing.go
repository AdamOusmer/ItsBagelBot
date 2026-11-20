// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package draw

import "time"

func earlier(a, b time.Time) time.Time {
	if a.Before(b) {
		return a
	}
	return b
}

func nextSlotDue(data snapshot, now time.Time) time.Time {
	next := settlementDeadline(data.round)
	if !data.round.ClaimPausedAt.IsZero() {
		next = now.Add(sweepPoll)
	}
	if at := verifyingNoticeAt(data); !at.IsZero() && hasWorkingSlots(data, now) {
		next = earlier(next, at)
	}
	for _, slot := range data.slots {
		if at := slotDue(data.round, slot, now); !at.IsZero() {
			next = earlier(next, at)
		}
	}
	return next
}

func settlementDeadline(r Round) time.Time {
	if r.Terms.Kind == KindRaffle {
		return r.SettledAt.Add(Pins.PayoutHorizon)
	}
	return r.SettledAt.Add(Pins.GiveawayHorizon)
}

func slotDue(r Round, slot Slot, now time.Time) time.Time {
	if slotTerminal(r, slot) || pausedClaim(r, slot) {
		return time.Time{}
	}
	deadline := settlementDeadline(r)
	switch slot.State {
	case SlotPending:
		return earlier(retryAt(slot, now), deadline)
	case SlotVerifying:
		return earlier(verifyDue(slot, now), deadline)
	case SlotWon:
		return earlier(wonDue(r, slot, now), deadline)
	case SlotNoShow:
		if r.Terms.RerollNoShows {
			return now
		}
		return earlier(noShowDeadline(slot), deadline)
	}
	return deadline
}

func verifyDue(slot Slot, now time.Time) time.Time {
	stuckAt := verifyStuckAt(slot)
	if now.Before(stuckAt) {
		return earlier(retryAt(slot, now), stuckAt)
	}
	return verifyDeadline(slot)
}

func verifyStuckAt(slot Slot) time.Time { return slot.Since.Add(Pins.VerifyStuck) }

func verifyDeadline(slot Slot) time.Time { return verifyStuckAt(slot).Add(Pins.ModActionTimeout) }

func wonDue(r Round, slot Slot, now time.Time) time.Time {
	if r.Terms.Kind == KindRaffle {
		return retryAt(slot, now)
	}
	return slot.Deadline.Truncate(time.Millisecond).Add(time.Millisecond)
}

func noShowDeadline(slot Slot) time.Time {
	return slot.Deadline.Add(Pins.ModActionTimeout)
}

func retryAt(slot Slot, now time.Time) time.Time {
	if slot.RetryAt.IsZero() {
		return now
	}
	return slot.RetryAt
}

func pausedClaim(r Round, slot Slot) bool {
	return slot.State == SlotWon && !r.ClaimPausedAt.IsZero() && !slot.Deadline.Before(r.ClaimTime(r.ClaimPausedAt, slot))
}
