// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package draw

import (
	"context"
	"time"

	"ItsBagelBot/pkg/valkey"
)

func (t Terms) remindersEnabled() bool { return t.Kind == KindRaffle || t.RemindEnabled }

func (s *Store) remind(ctx context.Context, data snapshot, now time.Time) error {
	r := data.round
	if !r.Terms.remindersEnabled() {
		return s.schedule(ctx, data, r.ClosesAt.Sub(now))
	}
	raw := data.stored.Round["remind_at"]
	at := fromMillis(parseInt(raw))
	if at.IsZero() {
		at = r.OpenedAt.Add(Pins.RemindInterval)
	}
	if now.Before(at) {
		return s.schedule(ctx, data, earlier(at, r.ClosesAt).Sub(now))
	}
	next := now.Add(Pins.RemindInterval)
	shift := phaseShift(r, PhaseOpen, earlier(next, r.ClosesAt))
	shift.Match = valkey.FieldValue{Field: "remind_at", Value: raw}
	shift.Fields = []valkey.FieldValue{timeField("remind_at", next)}
	ok, err := keysFor(r.Channel, r.ID).Shift(ctx, s.client, shift)
	if ok {
		s.cfg.Announcer.Announce(ctx, Notice{
			Event: NoticeRemind, Round: r, Entries: len(data.stored.Entries), TimeLeft: r.ClosesAt.Sub(now),
		})
	}
	return err
}
