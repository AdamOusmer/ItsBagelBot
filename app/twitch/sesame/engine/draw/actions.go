// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package draw

import (
	"context"
	"encoding/json"
	"strconv"
	"time"

	"ItsBagelBot/pkg/valkey"
)

type CloseRequest struct {
	Channel ChannelID
	Actor   ViewerID
	Winners *WinnerPolicy
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
}

type SlotAction struct {
	Channel ChannelID
	Actor   ViewerID
	Slot    int
}

func (s *Store) Close(ctx context.Context, req CloseRequest) (bool, error) {
	r, found, err := s.Joinable(ctx, req.Channel)
	if err != nil || !found {
		return false, err
	}
	now, err := valkey.ServerNow(ctx, s.client)
	if err != nil {
		return false, err
	}
	shift := phaseShift(r, PhaseClosing, now)
	action := Action{Kind: "close", Actor: req.Actor, At: now}
	if req.Winners != nil {
		raw, err := json.Marshal(req.Winners)
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
	shift := phaseShift(r, PhaseOpen, next)
	shift.Match = timeField("closes_at", r.ClosesAt)
	shift.Fields = []valkey.FieldValue{timeField("closes_at", next)}
	return s.amend(ctx, r, shift, Action{Kind: "extend", Actor: req.Actor, At: now, Value: shift.Fields[0].Value})
}

func (s *Store) Cancel(ctx context.Context, req ModAction) (bool, error) {
	r, err := s.cancellable(ctx, req)
	if err != nil || !cancellable(r.Phase) {
		return false, err
	}
	now, err := valkey.ServerNow(ctx, s.client)
	if err != nil {
		return false, err
	}
	action := Action{Kind: "cancel", Actor: req.Actor, At: now}
	applied, err := s.amend(ctx, r, phaseShift(r, PhaseCancelled, now), action)
	if applied || err != nil {
		return applied, err
	}
	if r.Phase == PhaseClosing {
		return false, nil
	}
	r.Phase = PhaseClosing
	return s.amend(ctx, r, phaseShift(r, PhaseCancelled, now), action)
}

func cancellable(phase Phase) bool { return phase == PhaseOpen || phase == PhaseClosing }

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
		return s.latest(ctx, req.Channel, roundFilter{phase: PhaseClosing})
	}
	r, err := s.metadata(ctx, req.Channel, id)
	if r.Channel != req.Channel {
		return Round{}, err
	}
	return r, err
}

func (s *Store) amend(ctx context.Context, r Round, shift valkey.PhaseShift, action Action) (bool, error) {
	raw, err := json.Marshal(action)
	if err != nil {
		return false, err
	}
	shift.Action = string(raw)
	return keysFor(r.Channel, r.ID).Shift(ctx, s.client, shift)
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

func (s *Store) changeSlots(ctx context.Context, req SlotAction, kind slotActionKind) (int, error) {
	r, err := s.latest(ctx, req.Channel, roundFilter{phase: PhaseSettling, kind: KindGiveaway})
	if err != nil || r.ID == "" {
		return 0, err
	}
	data, err := s.read(ctx, r.ID)
	if err != nil || data.round.Phase != PhaseSettling {
		return 0, err
	}
	now, err := valkey.ServerNow(ctx, s.client)
	if err != nil {
		return 0, err
	}
	change := slotEdit{action: Action{Kind: string(kind), Actor: req.Actor, At: now, Value: strconv.Itoa(req.Slot)},
		transitions: modTransitions(data, req, kind)}
	return s.commitEdit(ctx, data, change)
}

type slotActionKind string

type slotEdit struct {
	action      Action
	transitions []slotChange
}

func modTransitions(data snapshot, req SlotAction, kind slotActionKind) []slotChange {
	var changes []slotChange
	for _, slot := range data.slots {
		if req.Slot != 0 && slot.N != req.Slot {
			continue
		}
		switch {
		case kind == "reroll" && slot.State == SlotNoShow:
			changes = append(changes, slotChange{prior: slot, next: rerolled(slot)})
		case kind == "end" && !slotTerminal(data.round, slot):
			next := slot
			next.State = SlotEnded
			changes = append(changes, slotChange{prior: slot, next: next, index: claimDrop(slot)})
		}
	}
	return changes
}

func (s *Store) commitEdit(ctx context.Context, data snapshot, edit slotEdit) (int, error) {
	if len(edit.transitions) == 0 {
		return 0, nil
	}
	raw, err := json.Marshal(edit.action)
	if err != nil {
		return 0, err
	}
	ok, err := s.commitSlots(ctx, data, slotBatch{changes: edit.transitions, action: string(raw), now: edit.action.At})
	if err != nil || !ok {
		return 0, err
	}
	return len(edit.transitions), nil
}
