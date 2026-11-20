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
	if slotTerminal(r, slot) {
		return time.Time{}
	}
	deadline := settlementDeadline(r)
	switch slot.State {
	case SlotPending:
		return earlier(retryAt(slot, now), deadline)
	case SlotVerifying:
		return earlier(verifyDue(r, slot, now), deadline)
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

func verifyDue(r Round, slot Slot, now time.Time) time.Time {
	stuckAt := slot.Since.Add(Pins.VerifyStuck)
	if now.Before(stuckAt) {
		return earlier(retryAt(slot, now), stuckAt)
	}
	if r.Terms.Kind == KindRaffle {
		return settlementDeadline(r)
	}
	return stuckAt.Add(Pins.ModActionTimeout)
}

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
