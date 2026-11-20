// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package draw

import (
	"context"
	"strconv"
	"strings"
	"time"

	"ItsBagelBot/pkg/codec"
	"ItsBagelBot/pkg/valkey"
)

type CloseRequest struct {
	Channel ChannelID
	Actor   ViewerID
	Winners *WinnerPolicy
	Round   RoundID
	Kind    Kind
}

type ExtendRequest struct {
	Channel ChannelID
	Actor   ViewerID
	Until   time.Time
	Round   RoundID
	Kind    Kind
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
	Login   string
	Round   RoundID
	Kind    Kind
}

func (s *Store) Close(ctx context.Context, req CloseRequest) (bool, error) {
	r, err := s.joinRound(ctx, req.Channel)
	if err != nil || !r.targetable(r.Phase == PhaseOpen, req.Kind, req.Round) {
		return false, err
	}
	now, err := valkey.ServerNow(ctx, s.client)
	if err != nil {
		return false, err
	}
	if r.Terms.Kind == KindRaffle && !now.Before(r.ClosesAt) {
		return false, nil
	}
	override, err := req.winnerOverride()
	if err != nil {
		return false, err
	}
	shift := phaseShift(r, PhaseClosing, now)
	if override != "" {
		shift.Fields = []valkey.FieldValue{{Field: "winner_override", Value: override}}
	}
	return s.amend(ctx, r, shift, Action{Kind: "close", Actor: req.Actor, At: now, Value: override})
}

func (r CloseRequest) winnerOverride() (string, error) {
	if r.Winners == nil {
		return "", nil
	}
	raw, err := codec.Marshal(r.Winners)
	return string(raw), err
}

func (s *Store) Extend(ctx context.Context, req ExtendRequest) (bool, error) {
	r, found, err := s.Joinable(ctx, req.Channel)
	if err != nil || !r.targetable(found, req.Kind, req.Round) {
		return false, err
	}
	now, err := valkey.ServerNow(ctx, s.client)
	if err != nil {
		return false, err
	}
	next := r.ClosesAt.Add(time.Minute)
	bound := r.OpenedAt.Add(r.Terms.maxWindow())
	if !req.Until.IsZero() {
		bound = earlier(bound, req.Until)
	}
	if next.After(bound) {
		return false, nil
	}
	if ok, err := s.extendCutoff(ctx, r, next); err != nil || !ok {
		return false, err
	}
	shift := phaseShift(r, PhaseOpen, now)
	shift.Match = timeField("closes_at", r.ClosesAt)
	shift.Fields = []valkey.FieldValue{timeField("closes_at", next)}
	return s.amend(ctx, r, shift, Action{Kind: "extend", Actor: req.Actor, At: now, Value: shift.Fields[0].Value})
}

func (s *Store) extendCutoff(ctx context.Context, r Round, until time.Time) (bool, error) {
	id := r.Terms.StandingsID()
	if id == "" {
		return true, nil
	}
	if s.cfg.Snapshots == nil {
		return false, ErrInvalidTerms
	}
	return s.cfg.Snapshots.ExtendSnapshot(ctx, SnapshotExtension{Channel: r.Channel, ID: id, ExpiresAt: until})
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
	if err != nil || !r.needsClosingRetry(applied) {
		return r, applied, err
	}
	r.Phase = PhaseClosing
	applied, err = s.amend(ctx, r, phaseShift(r, PhaseCancelled, now), action)
	return r, applied, err
}

func (r Round) needsClosingRetry(applied bool) bool {
	return !applied && r.Phase == PhaseOpen
}

func (r Round) canCancel(kind Kind) bool {
	switch r.Phase {
	case PhaseOpen, PhaseClosing:
		return r.isKind(kind)
	default:
		return false
	}
}

func (s *Store) cancellable(ctx context.Context, req ModAction) (Round, error) {
	id, err := s.targetOrJoinableID(ctx, req.Channel, req.Round)
	if err != nil {
		return Round{}, err
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

func (r Round) targetable(found bool, kind Kind, id RoundID) bool {
	if !found || !r.isKind(kind) {
		return false
	}
	return id == "" || id == r.ID
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
	action := Action{Kind: string(kind), Actor: req.Actor, At: now, Value: req.selection()}
	edit := slotEdit{action: action, transitions: modTransitions(data, req, action)}
	stampSlotTimes(data, edit.transitions, now, 0)
	return s.commitEdit(ctx, data, edit)
}

func (req SlotAction) selection() string {
	if req.Login != "" {
		return "@" + req.Login
	}
	return strconv.Itoa(req.Slot)
}

func (s *Store) slotActionRound(ctx context.Context, req SlotAction, kind slotActionKind) (snapshot, error) {
	id, err := s.slotActionRoundID(ctx, req, kind)
	if err != nil || id == "" {
		return snapshot{}, err
	}
	data, err := s.read(ctx, id)
	if data.round.Channel != req.Channel || !actionMatches(data.round, req, kind) {
		return snapshot{}, err
	}
	return data, err
}

func (s *Store) slotActionRoundID(ctx context.Context, req SlotAction, kind slotActionKind) (RoundID, error) {
	if req.Round != "" {
		return req.Round, nil
	}
	r, err := s.latest(ctx, req.Channel, roundFilter{phase: PhaseSettling, kind: actionRoundKind(req, kind)})
	return r.ID, err
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
		if !req.Matches(slot) {
			continue
		}
		if next := modSlot(data.round, slot, action); next.State != "" {
			changes = append(changes, slotChange{prior: slot, next: next, index: claimIndex(slot, next)})
		}
	}
	return changes
}

func (req SlotAction) Matches(slot Slot) bool {
	if req.Login != "" {
		return namedWinner(slot.State) && strings.EqualFold(req.Login, slot.Login)
	}
	return req.Slot == 0 || slot.N == req.Slot
}

func namedWinner(state SlotState) bool {
	switch state {
	case SlotWon, SlotNoShow, SlotClaimed:
		return true
	default:
		return false
	}
}

func modSlot(r Round, slot Slot, action Action) Slot {
	switch {
	case action.Kind == "reroll" && canRerollSlot(slot, action):
		return rerolled(slot)
	case action.Kind == "skip_rule" && CanSkipRule(slot, action.At):
		return finalSlot(slot, SlotWon)
	case action.Kind == "end" && !slotTerminal(r, slot):
		return finalSlot(slot, SlotEnded)
	default:
		return Slot{}
	}
}

func canRerollSlot(slot Slot, action Action) bool {
	if rerollableNoShow(slot, action.At) {
		return true
	}
	return action.Value != "0" && CanSkipRule(slot, action.At)
}

func CanSkipRule(slot Slot, now time.Time) bool {
	if slot.State != SlotVerifying {
		return false
	}
	return !now.Before(verifyStuckAt(slot)) && now.Before(verifyDeadline(slot))
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
