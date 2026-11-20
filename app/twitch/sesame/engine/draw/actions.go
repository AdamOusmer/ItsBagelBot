// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package draw

import (
	"context"
	"strconv"
	"time"

	"ItsBagelBot/pkg/codec"
	"ItsBagelBot/pkg/valkey"
)

type CloseRequest struct {
	Channel ChannelID
	Actor   ViewerID
	Winners *WinnerPolicy
	Kind    Kind
}

type ExtendRequest struct {
	Channel ChannelID
	Actor   ViewerID
	Until   time.Time
}

type ModAction struct {
	Channel ChannelID
	Actor   ViewerID
	Round   RoundID
	Kind    Kind
}

type SlotAction struct {
	Channel ChannelID
	Actor   ViewerID
	Slot    int
	Round   RoundID
	Kind    Kind
}

func (s *Store) Close(ctx context.Context, req CloseRequest) (bool, error) {
	r, found, err := s.Joinable(ctx, req.Channel)
	if err != nil || !found || !r.isKind(req.Kind) {
		return false, err
	}
	now, err := valkey.ServerNow(ctx, s.client)
	if err != nil {
		return false, err
	}
	shift := phaseShift(r, PhaseClosing, now)
	action := Action{Kind: "close", Actor: req.Actor, At: now}
	if req.Winners != nil {
		raw, err := codec.Marshal(req.Winners)
		if err != nil {
			return false, err
		}
		action.Value = string(raw)
		shift.Fields = []valkey.FieldValue{{Field: "winner_override", Value: action.Value}}
	}
	return s.amend(ctx, r, shift, action)
}

func (s *Store) Extend(ctx context.Context, req ExtendRequest) (bool, error) {
	r, found, err := s.Joinable(ctx, req.Channel)
	if err != nil || !found {
		return false, err
	}
	now, err := valkey.ServerNow(ctx, s.client)
	if err != nil {
		return false, err
	}
	next := r.ClosesAt.Add(time.Minute)
	bound := r.OpenedAt.Add(Pins.MaxOpen)
	if !req.Until.IsZero() {
		bound = earlier(bound, req.Until)
	}
	if next.After(bound) {
		return false, nil
	}
	shift := phaseShift(r, PhaseOpen, now)
	shift.Match = timeField("closes_at", r.ClosesAt)
	shift.Fields = []valkey.FieldValue{timeField("closes_at", next)}
	return s.amend(ctx, r, shift, Action{Kind: "extend", Actor: req.Actor, At: now, Value: shift.Fields[0].Value})
}

func (s *Store) Cancel(ctx context.Context, req ModAction) (bool, error) {
	_, applied, err := s.CancelRound(ctx, req)
	return applied, err
}

func (s *Store) CancelRound(ctx context.Context, req ModAction) (Round, bool, error) {
	r, err := s.cancellable(ctx, req)
	if err != nil || !r.canCancel(req.Kind) {
		return r, false, err
	}
	now, err := valkey.ServerNow(ctx, s.client)
	if err != nil {
		return r, false, err
	}
	action := Action{Kind: "cancel", Actor: req.Actor, At: now}
	applied, err := s.amend(ctx, r, phaseShift(r, PhaseCancelled, now), action)
	if applied || err != nil || r.Phase == PhaseClosing {
		return r, applied, err
	}
	r.Phase = PhaseClosing
	applied, err = s.amend(ctx, r, phaseShift(r, PhaseCancelled, now), action)
	return r, applied, err
}

func (r Round) canCancel(kind Kind) bool {
	return (r.Phase == PhaseOpen || r.Phase == PhaseClosing) && r.isKind(kind)
}

func (s *Store) cancellable(ctx context.Context, req ModAction) (Round, error) {
	id := req.Round
	if id == "" {
		var err error
		id, err = s.joinableID(ctx, req.Channel)
		if err != nil {
			return Round{}, err
		}
	}
	if id == "" {
		return s.latest(ctx, req.Channel, roundFilter{phase: PhaseClosing, kind: req.Kind})
	}
	r, err := s.metadata(ctx, req.Channel, id)
	if err != nil || r.Channel != req.Channel {
		return Round{}, err
	}
	if req.Round == "" && !r.isKind(req.Kind) {
		return s.latest(ctx, req.Channel, roundFilter{phase: PhaseClosing, kind: req.Kind})
	}
	return r, nil
}

func (r Round) isKind(kind Kind) bool {
	return kind == "" || r.Terms.Kind == kind
}

func (s *Store) amend(ctx context.Context, r Round, shift valkey.PhaseShift, action Action) (bool, error) {
	raw, err := codec.Marshal(action)
	if err != nil {
		return false, err
	}
	shift.Action = string(raw)
	return s.shiftPhase(ctx, r, shift)
}

func (s *Store) shiftPhase(ctx context.Context, r Round, shift valkey.PhaseShift) (bool, error) {
	ok, err := keysFor(r.Channel, r.ID).Shift(ctx, s.client, shift)
	if ok && shift.Release {
		s.cfg.Phases.LeftOpen(ctx, r)
	}
	return ok, err
}

func phaseShift(r Round, to Phase, at time.Time) valkey.PhaseShift {
	return valkey.PhaseShift{From: string(r.Phase), To: string(to), RoundID: string(r.ID), TTL: roundTTL,
		Release: r.Phase == PhaseOpen && to != PhaseOpen, Wake: valkey.DrawWake{Channel: wakeChannel, At: at}}
}

func (s *Store) EndSlots(ctx context.Context, action SlotAction) (int, error) {
	return s.changeSlots(ctx, action, "end")
}

func (s *Store) Reroll(ctx context.Context, action SlotAction) (int, error) {
	return s.changeSlots(ctx, action, "reroll")
}

func (s *Store) SkipRule(ctx context.Context, action SlotAction) (int, error) {
	return s.changeSlots(ctx, action, "skip_rule")
}

type slotActionKind string

type slotEdit struct {
	action      Action
	transitions []slotChange
}

func (s *Store) changeSlots(ctx context.Context, req SlotAction, kind slotActionKind) (int, error) {
	data, err := s.slotActionRound(ctx, req, kind)
	if err != nil || data.round.Phase != PhaseSettling {
		return 0, err
	}
	now, err := valkey.ServerNow(ctx, s.client)
	if err != nil || !now.Before(settlementDeadline(data.round)) {
		return 0, err
	}
	action := Action{Kind: string(kind), Actor: req.Actor, At: now, Value: strconv.Itoa(req.Slot)}
	edit := slotEdit{action: action, transitions: modTransitions(data, req, action)}
	stampWinners(data, edit.transitions, now)
	return s.commitEdit(ctx, data, edit)
}

func (s *Store) slotActionRound(ctx context.Context, req SlotAction, kind slotActionKind) (snapshot, error) {
	id := req.Round
	if id == "" {
		r, err := s.latest(ctx, req.Channel, roundFilter{phase: PhaseSettling, kind: actionRoundKind(req, kind)})
		if err != nil || r.ID == "" {
			return snapshot{}, err
		}
		id = r.ID
	}
	data, err := s.read(ctx, id)
	if data.round.Channel != req.Channel || !actionMatches(data.round, req, kind) {
		return snapshot{}, err
	}
	return data, err
}

func actionRoundKind(req SlotAction, kind slotActionKind) Kind {
	if kind == "end" {
		return KindGiveaway
	}
	if kind == "reroll" && req.Kind == "" {
		return KindGiveaway
	}
	return req.Kind
}

func actionMatches(r Round, req SlotAction, kind slotActionKind) bool {
	if kind == "end" {
		return r.Terms.Kind == KindGiveaway
	}
	return req.Kind == "" || r.Terms.Kind == req.Kind
}

func modTransitions(data snapshot, req SlotAction, action Action) []slotChange {
	var changes []slotChange
	for _, slot := range data.slots {
		if req.Slot != 0 && slot.N != req.Slot {
			continue
		}
		if next := modSlot(data.round, slot, action); next.State != "" {
			changes = append(changes, slotChange{prior: slot, next: next, index: claimIndex(slot, next)})
		}
	}
	return changes
}

func modSlot(r Round, slot Slot, action Action) Slot {
	stuck := verifiableByMod(slot, action.At)
	switch action.Kind {
	case "reroll":
		if stuck || rerollableNoShow(slot, action.At) {
			return rerolled(slot)
		}
	case "skip_rule":
		if stuck {
			return finalSlot(slot, SlotWon)
		}
	case "end":
		if !slotTerminal(r, slot) {
			return finalSlot(slot, SlotEnded)
		}
	}
	return Slot{}
}

func verifiableByMod(slot Slot, now time.Time) bool {
	return slot.State == SlotVerifying && !now.Before(verifyStuckAt(slot)) && now.Before(verifyDeadline(slot))
}

func rerollableNoShow(slot Slot, now time.Time) bool {
	return slot.State == SlotNoShow && now.Before(noShowDeadline(slot))
}

func (s *Store) commitEdit(ctx context.Context, data snapshot, edit slotEdit) (int, error) {
	if len(edit.transitions) == 0 {
		return 0, nil
	}
	raw, err := codec.Marshal(edit.action)
	if err != nil {
		return 0, err
	}
	ok, err := s.commitSlots(ctx, data, slotBatch{changes: edit.transitions, action: string(raw), now: edit.action.At})
	if err != nil || !ok {
		return 0, err
	}
	s.announceChanges(ctx, data, edit.transitions)
	return len(edit.transitions), nil
}
