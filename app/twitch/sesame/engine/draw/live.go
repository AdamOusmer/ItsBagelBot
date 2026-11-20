// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package draw

import (
	"context"

	"ItsBagelBot/pkg/valkey"
)

type RoundView struct {
	Round
	Slots   []Slot
	Entries int
	Refused int64
}

func (s *Store) Live(ctx context.Context, ch ChannelID) ([]RoundView, error) {
	ids, err := valkey.ActiveRounds(ctx, s.client, activeKey(ch))
	if err != nil {
		return nil, err
	}
	var rounds []RoundView
	for _, id := range ids {
		data, err := s.read(ctx, RoundID(id))
		if err != nil {
			return nil, err
		}
		if livePhase(data.round.Phase) {
			rounds = append(rounds, RoundView{Round: data.round, Slots: data.slots,
				Entries: len(data.stored.Entries), Refused: data.stored.Refused})
		}
	}
	return rounds, nil
}

func livePhase(phase Phase) bool {
	switch phase {
	case PhaseOpen, PhaseClosing, PhaseSettling:
		return true
	default:
		return false
	}
}
