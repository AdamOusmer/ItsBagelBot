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
	if r.Terms.Kind == KindRaffle && !r.Terms.MidCountDisabled && !midSent {
		reminders = append(reminders, reminder{r.OpenedAt.Add(r.ClosesAt.Sub(r.OpenedAt) / 2).Truncate(time.Millisecond), NoticeMidCount})
	}
	if r.regularReminders() {
		if at.IsZero() {
			at = r.OpenedAt.Add(Pins.RemindInterval)
		}
		reminders = append(reminders, reminder{at, NoticeRemind})
	}
	return reminders
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
	shift := r.reminderShift(next, at, now)
	shift.Match = valkey.FieldValue{Field: "revision", Value: data.stored.Round["revision"]}
	ok, err := keysFor(r.Channel, r.ID).Shift(ctx, s.client, shift)
	if ok {
		s.cfg.Announcer.Announce(ctx, Notice{
			Event: next.event, Round: r, Entries: len(data.stored.Entries), TimeLeft: r.ClosesAt.Sub(now),
		})
	}
	return err
}

func (r Round) reminderShift(next reminder, at, now time.Time) valkey.PhaseShift {
	shift := phaseShift(r, PhaseOpen, next.at)
	if next.event == NoticeMidCount {
		shift.Fields = append(shift.Fields, valkey.FieldValue{Field: "mid_count_sent", Value: "1"})
	}
	if advanceReminder(r, next, at) {
		shift.Fields = append(shift.Fields, timeField("remind_at", now.Add(Pins.RemindInterval)))
	}
	return shift
}

func advanceReminder(r Round, next reminder, at time.Time) bool {
	if next.event == NoticeRemind {
		return true
	}
	if at.IsZero() {
		at = r.OpenedAt.Add(Pins.RemindInterval)
	}
	return r.regularReminders() && at.Equal(next.at)
}
