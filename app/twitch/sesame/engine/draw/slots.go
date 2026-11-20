// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package draw

import (
	"context"
	"encoding/json"
	"errors"
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
	entry     Entry
}

type slotStep func(context.Context, *Store, slotWork) Slot

func slotSteps(r Round) map[SlotState]slotStep {
	return map[SlotState]slotStep{
		SlotPending: reservePick, SlotVerifying: verifyPick, SlotWon: settlers[r.Terms.Kind].afterWin, SlotNoShow: stepNoShow,
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
			next := s.stepSlot(ctx, w, steps[w.slot.State])
			changes[i] = slotChange{prior: w.slot, next: next, index: claimIndex(w.slot, next)}
			return nil
		})
	}
	_ = tasks.Wait()
	return slices.DeleteFunc(changes, func(c slotChange) bool { return c.next.State == "" })
}

func (s *Store) stepSlot(ctx context.Context, w slotWork, step slotStep) Slot {
	if w.now.Before(settlementDeadline(w.round)) {
		return step(ctx, s, w)
	}
	if w.round.Terms.Kind == KindRaffle {
		return stopRaffleSlot(w.slot)
	}
	return finalSlot(w.slot, SlotEnded)
}

func stopRaffleSlot(slot Slot) Slot {
	if slot.State == SlotWon {
		return finalSlot(slot, SlotUnpaid)
	}
	return finalSlot(slot, SlotEnded)
}

func (s *Store) prepareSlots(data snapshot, now time.Time) []slotWork {
	drawn := drawnViewers(data.slots)
	weights := make(map[ViewerID]int64, len(data.stored.Entries))
	for _, entry := range data.stored.Entries {
		weights[ViewerID(entry.Member)] = entry.Score
	}
	var work []slotWork
	for _, slot := range data.slots {
		at := slotDue(data.round, slot, now)
		if at.IsZero() || at.After(now) {
			continue
		}
		w := slotWork{round: data.round, slot: slot, now: now}
		switch slot.State {
		case SlotPending:
			w.candidate, _ = pick(data.stored.Entries, drawn, s.cfg.Random)
			w.login = data.stored.Labels[string(w.candidate)]
			drawn[w.candidate] = true
		case SlotVerifying:
			w.entry.Weight = weights[slot.Viewer]
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
		return reserveRafflePick(ctx, s.cfg.Payer, w)
	}
	return startVerification(w, slot)
}

func reserveRafflePick(ctx context.Context, payer Payer, w slotWork) Slot {
	slot := w.slot
	instance, err := payer.Instance(ctx, w.round.Channel, w.candidate)
	switch {
	case errors.Is(err, ErrAccountInstanceUnavailable):
		return stopRaffleSlot(slot)
	case err != nil:
		return retry(slot, w.now.Add(verifyRetry))
	default:
		slot.Instance = instance
		return startVerification(w, slot)
	}
}

func startVerification(w slotWork, slot Slot) Slot {
	slot.State, slot.Viewer, slot.Login = SlotVerifying, w.candidate, w.login
	slot.Since, slot.RetryAt = w.now, time.Time{}
	slot.History = append(slices.Clone(slot.History), Candidate{Viewer: slot.Viewer, Generation: slot.Generation})
	return slot
}

func verifyPick(ctx context.Context, s *Store, w slotWork) Slot {
	slot := w.slot
	if !w.now.Before(verifyStuckAt(slot)) {
		return finalSlot(slot, SlotEnded)
	}
	verdict, err := s.cfg.Verifier.Verify(ctx, Check{Round: w.round, Viewer: slot.Viewer, Login: slot.Login, Entry: w.entry})
	if err != nil {
		return retry(slot, w.now.Add(verifyDelay(err)))
	}
	if verdict.Pass {
		return finalSlot(slot, SlotWon)
	}
	slot.History = slices.Clone(slot.History)
	candidate := &slot.History[len(slot.History)-1]
	candidate.Reason, candidate.Detail = verdict.Reason, verdict.Detail
	return pendingSlot(slot, slot.Generation)
}

func verifyDelay(err error) time.Duration {
	var hint interface{ RetryAfter() time.Duration }
	if errors.As(err, &hint) {
		return max(hint.RetryAfter(), time.Second)
	}
	return verifyRetry
}

func finalSlot(slot Slot, state SlotState) Slot {
	slot.State, slot.RetryAt = state, time.Time{}
	return slot
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

func stepNoShow(_ context.Context, _ *Store, w slotWork) Slot {
	if w.round.Terms.RerollNoShows && rerollableNoShow(w.slot, w.now) {
		return rerolled(w.slot)
	}
	return finalSlot(w.slot, SlotEnded)
}

func claimIndex(prior, next Slot) valkey.ClaimIndex {
	if next.State != SlotWon {
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
	spec.Fields = workingFields(data, batch)
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

func workingFields(data snapshot, batch slotBatch) []valkey.FieldValue {
	if hasWorkingSlots(data, batch.now) {
		return nil
	}
	for _, change := range batch.changes {
		if slotWorking(data.round, change.next, batch.now) {
			return []valkey.FieldValue{timeField("working_since", batch.now), {Field: "verifying_notice"}}
		}
	}
	return nil
}

func indexedSlot(change slotChange) Slot {
	if change.index == valkey.ClaimAdd {
		return change.next
	}
	return change.prior
}

func (s *Store) announceChanges(ctx context.Context, r Round, changes []slotChange) {
	events := map[SlotState]NoticeEvent{
		settlers[r.Terms.Kind].announceOn: NoticeWinners, SlotNoShow: NoticeNoShow, SlotUnfilled: NoticeUnfilled, SlotUnpaid: NoticeUnpaid,
	}
	notices := make(map[NoticeEvent][]Slot)
	for _, change := range changes {
		if event := events[change.next.State]; change.next.State != change.prior.State && event != "" {
			notices[event] = append(notices[event], change.next)
		}
	}
	for _, event := range []NoticeEvent{NoticeNoShow, NoticeWinners, NoticeUnfilled, NoticeUnpaid} {
		if slots := notices[event]; len(slots) > 0 {
			s.cfg.Announcer.Announce(ctx, Notice{Event: event, Round: r, Slots: slots})
		}
	}
}
