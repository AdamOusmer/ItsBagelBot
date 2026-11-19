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
	next := now.Add(roundTTL)
	if data.stored.Round["verifying_notice"] == "" && hasWorkingSlots(data) {
		next = earlier(next, data.round.SettledAt.Add(Pins.VerifyingNotice))
	}
	for _, slot := range data.slots {
		if at := slotDue(data.round, slot, now); !at.IsZero() {
			next = earlier(next, at)
		}
	}
	return next
}

func slotDue(r Round, slot Slot, now time.Time) time.Time {
	switch slot.State {
	case SlotPending:
		return retryAt(slot, now)
	case SlotVerifying:
		if now.Before(slot.Since.Add(Pins.VerifyStuck)) {
			return retryAt(slot, now)
		}
	case SlotWon:
		return wonDue(r, slot, now)
	case SlotNoShow:
		if r.Terms.RerollNoShows {
			return now
		}
	}
	return time.Time{}
}

func wonDue(r Round, slot Slot, now time.Time) time.Time {
	if r.Terms.Kind == KindRaffle {
		return earlier(retryAt(slot, now), r.SettledAt.Add(Pins.PayoutHorizon))
	}
	if !slot.Deadline.IsZero() {
		return slot.Deadline.Truncate(time.Millisecond).Add(time.Millisecond)
	}
	return time.Time{}
}

func retryAt(slot Slot, now time.Time) time.Time {
	if slot.RetryAt.IsZero() {
		return now
	}
	return slot.RetryAt
}
