// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package draw

import (
	"context"
	"encoding/json"
	"slices"
	"strconv"

	"ItsBagelBot/pkg/valkey"
)

type snapshot struct {
	round  Round
	slots  []Slot
	stored valkey.RoundRead
}

func (s *Store) read(ctx context.Context, id RoundID) (snapshot, error) {
	keys := keysFor(0, id)
	raw, err := keys.Read(ctx, s.client)
	return decodeSnapshot(id, raw, err)
}

func decodeSnapshot(id RoundID, raw valkey.RoundRead, err error) (snapshot, error) {
	if err != nil || !raw.Found {
		return snapshot{}, err
	}
	r, err := decodeRound(id, raw.Round)
	if err != nil {
		return snapshot{}, err
	}
	slots := make([]Slot, 0, len(raw.Slots))
	for _, field := range raw.Slots {
		var slot Slot
		if err := json.Unmarshal([]byte(field), &slot); err != nil {
			return snapshot{}, err
		}
		slots = append(slots, slot)
	}
	slices.SortFunc(slots, func(a, b Slot) int { return a.N - b.N })
	return snapshot{round: r, slots: slots, stored: raw}, nil
}

func (s *Store) metadata(ctx context.Context, ch ChannelID, id RoundID) (Round, error) {
	keys := keysFor(ch, id)
	raw, err := keys.Metadata(ctx, s.client)
	if err != nil || len(raw) == 0 {
		return Round{}, err
	}
	return decodeRound(id, raw)
}

func decodeRound(id RoundID, raw map[string]string) (Round, error) {
	r := Round{ID: id, Channel: ChannelID(parseUint(raw["channel"])), Phase: Phase(raw["phase"]),
		OpenedAt: fromMillis(parseInt(raw["opened_at"])), ClosesAt: fromMillis(parseInt(raw["closes_at"])),
		SettledAt: fromMillis(parseInt(raw["settled_at"])), Winners: int(parseInt(raw["winners"]))}
	if err := json.Unmarshal([]byte(raw["terms"]), &r.Terms); err != nil {
		return Round{}, err
	}
	if actions := raw["actions"]; actions != "" {
		err := json.Unmarshal([]byte(actions), &r.Actions)
		return r, err
	}
	return r, nil
}

func parseInt(raw string) int64 {
	n, _ := strconv.ParseInt(raw, 10, 64)
	return n
}

func parseUint(raw string) uint64 {
	n, _ := strconv.ParseUint(raw, 10, 64)
	return n
}

type roundFilter struct {
	phase Phase
	kind  Kind
}

func (f roundFilter) matches(r Round) bool {
	return r.Phase == f.phase && (f.kind == "" || r.Terms.Kind == f.kind)
}

func (s *Store) latest(ctx context.Context, ch ChannelID, filter roundFilter) (Round, error) {
	ids, err := valkey.ActiveRounds(ctx, s.client, activeKey(ch))
	if err != nil {
		return Round{}, err
	}
	for _, id := range ids {
		r, err := s.metadata(ctx, ch, RoundID(id))
		if err != nil || filter.matches(r) {
			return r, err
		}
	}
	return Round{}, nil
}

func (s *Store) Settling(ctx context.Context, ch ChannelID, kind Kind) (Round, []Slot, bool, error) {
	r, err := s.latest(ctx, ch, roundFilter{phase: PhaseSettling, kind: kind})
	if err != nil || r.ID == "" {
		return Round{}, nil, false, err
	}
	raw, err := keysFor(ch, r.ID).View(ctx, s.client)
	data, err := decodeSnapshot(r.ID, raw, err)
	return data.round, data.slots, data.round.Phase == PhaseSettling, err
}
