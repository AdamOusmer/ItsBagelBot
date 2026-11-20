// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package draw

import (
	"context"
	"time"

	"ItsBagelBot/pkg/valkey"
)

type reminder struct {
	at    time.Time
	event NoticeEvent
}

func (r Round) reminders(at time.Time, midSent bool) []reminder {
	var reminders []reminder
	if r.midCountEnabled() && !midSent {
		reminders = append(reminders, reminder{r.OpenedAt.Add(r.ClosesAt.Sub(r.OpenedAt) / 2).Truncate(time.Millisecond), NoticeMidCount})
	}
	if r.regularReminders() {
		reminders = append(reminders, reminder{r.regularReminderAt(at), NoticeRemind})
	}
	return reminders
}

func (r Round) midCountEnabled() bool {
	return r.Terms.Kind == KindRaffle && !r.Terms.MidCountDisabled
}

func (r Round) regularReminderAt(at time.Time) time.Time {
	if at.IsZero() {
		return r.OpenedAt.Add(Pins.RemindInterval)
	}
	return at
}

func (r Round) regularReminders() bool {
	if r.Terms.Kind == KindRaffle {
		return r.ClosesAt.Sub(r.OpenedAt) >= 2*Pins.RemindInterval
	}
	return r.Terms.RemindEnabled
}

func (r Round) nextReminder(at time.Time, midSent bool) reminder {
	next := reminder{at: r.ClosesAt}
	for _, candidate := range r.reminders(at, midSent) {
		if candidate.at.Before(next.at) {
			next = candidate
		}
	}
	return next
}

func (s *Store) remind(ctx context.Context, data snapshot, now time.Time) error {
	r := data.round
	at := fromMillis(parseInt(data.stored.Round["remind_at"]))
	midSent := data.stored.Round["mid_count_sent"] != ""
	next := r.nextReminder(at, midSent)
	if now.Before(next.at) || next.event == "" {
		return s.schedule(ctx, data, next.at.Sub(now))
	}
	shift := r.reminderShift(next, reminderState{at: at, midSent: midSent}, now)
	shift.Match = valkey.FieldValue{Field: "revision", Value: data.stored.Round["revision"]}
	ok, err := keysFor(r.Channel, r.ID).Shift(ctx, s.client, shift)
	if ok && shouldAnnounceReminder(next.event, data.stored.EntryCount) {
		s.cfg.Announcer.Announce(ctx, Notice{
			Event: next.event, Round: r, Entries: len(data.stored.Entries), TimeLeft: r.ClosesAt.Sub(now),
		})
	}
	return err
}

type reminderState struct {
	at      time.Time
	midSent bool
}

func shouldAnnounceReminder(event NoticeEvent, entries int64) bool {
	return event != NoticeMidCount || entries > 0
}

func (r Round) reminderShift(fired reminder, state reminderState, now time.Time) valkey.PhaseShift {
	shift := phaseShift(r, PhaseOpen, now)
	if fired.event == NoticeMidCount {
		state.midSent = true
		shift.Fields = append(shift.Fields, valkey.FieldValue{Field: "mid_count_sent", Value: "1"})
	}
	if r.advanceReminder(fired, state.at) {
		state.at = now.Add(Pins.RemindInterval)
		shift.Fields = append(shift.Fields, timeField("remind_at", state.at))
	}
	shift.Wake.At = r.nextReminder(state.at, state.midSent).at
	return shift
}

func (r Round) advanceReminder(fired reminder, at time.Time) bool {
	if fired.event == NoticeRemind {
		return true
	}
	return r.regularReminders() && r.regularReminderAt(at).Equal(fired.at)
}
