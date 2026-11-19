// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package draw

import (
	"context"
	"encoding/json"
	"slices"
	"strconv"
	"time"

	"ItsBagelBot/pkg/valkey"

	"golang.org/x/sync/errgroup"
)

const (
	verifyRetry = 5 * time.Second
	payoutRetry = 10 * time.Second
)

type slotChange struct {
	prior Slot
	next  Slot
	index valkey.ClaimIndex
}

type slotBatch struct {
	changes []slotChange
	action  string
	now     time.Time
}

type slotWork struct {
	round     Round
	slot      Slot
	now       time.Time
	candidate ViewerID
	login     string
}

type slotStep func(context.Context, *Store, slotWork) Slot

func slotSteps(r Round) map[SlotState]slotStep {
	return map[SlotState]slotStep{
		SlotPending: reservePick, SlotVerifying: verifyPick, SlotWon: settlers[r.Terms.Kind].afterWin, SlotNoShow: rerollNoShow,
	}
}

func (s *Store) advanceSlots(ctx context.Context, data snapshot, now time.Time) error {
	changes := s.slotChanges(ctx, data, now)
	if err := ctx.Err(); err != nil {
		return err
	}
	if len(changes) == 0 {
		return s.schedule(ctx, data, nextSlotDue(data, now).Sub(now))
	}
	at, err := valkey.ServerNow(ctx, s.client)
	if err != nil {
		return err
	}
	stampWinners(data, changes, at)
	ok, err := s.commitSlots(ctx, data, slotBatch{changes: changes, now: at})
	if ok {
		s.announceChanges(ctx, data.round, changes)
	}
	return err
}

func (s *Store) slotChanges(ctx context.Context, data snapshot, now time.Time) []slotChange {
	steps := slotSteps(data.round)
	work := s.prepareSlots(data, now)
	changes := make([]slotChange, len(work))
	var tasks errgroup.Group
	for i, w := range work {
		tasks.Go(func() error {
			next := steps[w.slot.State](ctx, s, w)
			changes[i] = slotChange{prior: w.slot, next: next, index: claimIndex(w.slot, next)}
			return nil
		})
	}
	_ = tasks.Wait()
	return slices.DeleteFunc(changes, func(c slotChange) bool { return c.next.State == "" })
}

func (s *Store) prepareSlots(data snapshot, now time.Time) []slotWork {
	drawn := drawnViewers(data.slots)
	var work []slotWork
	for _, slot := range data.slots {
		at := slotDue(data.round, slot, now)
		if at.IsZero() || at.After(now) {
			continue
		}
		w := slotWork{round: data.round, slot: slot, now: now}
		if slot.State == SlotPending {
			w.candidate, _ = pick(data.stored.Entries, drawn, s.cfg.Random)
			w.login = data.stored.Labels[string(w.candidate)]
			drawn[w.candidate] = true
		}
		work = append(work, w)
	}
	return work
}

func reservePick(ctx context.Context, s *Store, w slotWork) Slot {
	slot := w.slot
	if w.candidate == "" {
		slot.State = SlotUnfilled
		return slot
	}
	if w.round.Terms.Kind == KindRaffle {
		instance, err := s.cfg.Payer.Instance(ctx, w.round.Channel, w.candidate)
		if err != nil {
			return retry(slot, w.now.Add(verifyRetry))
		}
		slot.Instance = instance
	}
	slot.State, slot.Viewer, slot.Login = SlotVerifying, w.candidate, w.login
	slot.Since, slot.RetryAt = w.now, time.Time{}
	slot.History = append(slices.Clone(slot.History), Candidate{Viewer: slot.Viewer, Generation: slot.Generation})
	return slot
}

func verifyPick(ctx context.Context, s *Store, w slotWork) Slot {
	slot := w.slot
	if !w.now.Before(slot.Since.Add(Pins.VerifyStuck)) {
		return Slot{}
	}
	verdict, err := s.cfg.Verifier.Verify(ctx, Check{Round: w.round, Viewer: slot.Viewer, Login: slot.Login})
	if err != nil {
		return retry(slot, earlier(w.now.Add(verifyRetry), slot.Since.Add(Pins.VerifyStuck)))
	}
	slot.RetryAt = time.Time{}
	if verdict.Pass {
		slot.State = SlotWon
		return slot
	}
	slot.History = slices.Clone(slot.History)
	slot.History[len(slot.History)-1].Reason = verdict.Reason
	return pendingSlot(slot, slot.Generation)
}

func retry(slot Slot, at time.Time) Slot {
	slot.RetryAt = at
	return slot
}

func pendingSlot(slot Slot, generation int64) Slot {
	return Slot{N: slot.N, Generation: generation, State: SlotPending, History: slot.History}
}

func rerolled(slot Slot) Slot {
	return pendingSlot(slot, slot.Generation+1)
}

func rerollNoShow(_ context.Context, _ *Store, w slotWork) Slot {
	return rerolled(w.slot)
}

func claimIndex(prior, next Slot) valkey.ClaimIndex {
	if next.State == SlotNoShow {
		return claimDrop(prior)
	}
	return valkey.ClaimKeep
}

func stampWinners(data snapshot, changes []slotChange, at time.Time) {
	window := data.round.Terms.ClaimWindow
	line := parseInt(data.stored.Round["revision"]) + 1
	for i := range changes {
		change := &changes[i]
		if change.next.State != SlotWon || change.prior.State == SlotWon {
			continue
		}
		change.next.Line = line
		if window > 0 {
			change.next.Deadline = at.Add(window)
			change.index = valkey.ClaimAdd
		}
	}
}

func (s *Store) commitSlots(ctx context.Context, data snapshot, batch slotBatch) (bool, error) {
	r := data.round
	revision := data.stored.Round["revision"]
	spec := valkey.SlotBatch{Revision: &revision, RoundID: string(r.ID), Phase: string(PhaseSettling), Action: batch.action,
		Wake: valkey.DrawWake{Channel: wakeChannel, At: batch.now}}
	for _, change := range batch.changes {
		field := strconv.Itoa(change.prior.N)
		raw, err := json.Marshal(change.next)
		if err != nil {
			return false, err
		}
		spec.Shifts = append(spec.Shifts, valkey.SlotShift{
			Slot: field, Prior: data.stored.Slots[field], Next: string(raw), Index: change.index,
			Viewer: string(indexedSlot(change).Viewer), Ref: claimRef(r.ID, change.prior),
		})
	}
	return keysFor(r.Channel, r.ID).ShiftSlotBatch(ctx, s.client, spec)
}

func indexedSlot(change slotChange) Slot {
	if change.index == valkey.ClaimAdd {
		return change.next
	}
	return change.prior
}

func (s *Store) announceChanges(ctx context.Context, r Round, changes []slotChange) {
	events := map[SlotState]NoticeEvent{
		settlers[r.Terms.Kind].announceOn: NoticeWinners, SlotNoShow: NoticeNoShow, SlotUnfilled: NoticeUnfilled,
	}
	notices := make(map[NoticeEvent][]Slot)
	for _, change := range changes {
		if event := events[change.next.State]; change.next.State != change.prior.State && event != "" {
			notices[event] = append(notices[event], change.next)
		}
	}
	for _, event := range []NoticeEvent{NoticeNoShow, NoticeWinners, NoticeUnfilled} {
		if slots := notices[event]; len(slots) > 0 {
			s.cfg.Announcer.Announce(ctx, Notice{Event: event, Round: r, Slots: slots})
		}
	}
}
