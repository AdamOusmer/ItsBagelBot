// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"context"
	"time"

	"ItsBagelBot/app/twitch/sesame/engine/draw"
	"ItsBagelBot/internal/projection"
)

type RaffleLaunch struct {
	Points           int64
	Winners          draw.WinnerPolicy
	Multi            bool
	MidCountDisabled bool
}

type RaffleStatus struct {
	Open        bool
	Entrants    int64
	SecondsLeft int64
}

func RaffleTerms(l RaffleLaunch) draw.Terms {
	return draw.Terms{Kind: draw.KindRaffle, Prize: draw.Prize{Points: l.Points}, Winners: l.Winners, MultiRaffle: l.Multi, MidCountDisabled: l.MidCountDisabled}
}

func RaffleGame(r draw.Round) string {
	if r.Terms.MultiRaffle {
		return "mraffle"
	}
	return "raffle"
}

func LoyaltyOn(ctx context.Context, proj projection.Reader, ch uint64) (bool, error) {
	_, state, err := ModuleLookup{Proj: proj, BroadcasterID: ch, Name: LoyaltyModuleName, Absent: ModuleOff}.Resolve(ctx)
	return state == ModuleOn, err
}

func (d Deps) ReadRaffleStatus(ctx context.Context, ch uint64) (RaffleStatus, error) {
	if d.Draw == nil {
		return RaffleStatus{}, nil
	}
	rounds, err := d.Draw.Live(ctx, draw.ChannelID(ch))
	if err != nil {
		return RaffleStatus{}, err
	}
	now := time.Now()
	for _, r := range rounds {
		if raffleJoinableAt(r.Round, now) {
			left := (r.ClosesAt.Sub(now) + time.Second - 1) / time.Second
			return RaffleStatus{Open: true, Entrants: int64(r.Entries), SecondsLeft: int64(left)}, nil
		}
	}
	return RaffleStatus{}, nil
}

func raffleJoinableAt(r draw.Round, now time.Time) bool {
	return raffleOpen(r) && now.Before(r.ClosesAt)
}

func raffleOpen(r draw.Round) bool {
	return r.Terms.Kind == draw.KindRaffle && r.Phase == draw.PhaseOpen
}
