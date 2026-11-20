// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package draw

import (
	"time"

	drawrpc "ItsBagelBot/internal/domain/rpc/draw"
)

type PolicyPins struct {
	EntryWeight               int64
	MaxWinners                int
	GiveawayDefaultWinners    int
	GiveawayEntryWindow       time.Duration
	GiveawayMinEntryWindow    time.Duration
	GiveawayMaxPresets        int
	GiveawayPresetNameMax     int
	RaffleDefaultWinners      int
	MultiRaffleDefaultWinners int
	AutoDivisor               int
	ClaimWindowMin            time.Duration
	ClaimWindowMax            time.Duration
	ClaimWindowDefault        time.Duration
	PayoutHorizon             time.Duration
	VerifyingNotice           time.Duration
	VerifyStuck               time.Duration
	ModActionTimeout          time.Duration
	GiveawayHorizon           time.Duration
	RemindInterval            time.Duration
	MaxOpen                   time.Duration
	WinnerCooldown            time.Duration
}

var Pins = PolicyPins{
	EntryWeight: 100, MaxWinners: drawrpc.MaxWinners, GiveawayDefaultWinners: 1, RaffleDefaultWinners: 1,
	GiveawayEntryWindow: 3 * time.Minute, GiveawayMinEntryWindow: time.Second, GiveawayMaxPresets: 10, GiveawayPresetNameMax: 24,
	MultiRaffleDefaultWinners: 3, AutoDivisor: 10,
	ClaimWindowMin: 30 * time.Second, ClaimWindowMax: 15 * time.Minute, ClaimWindowDefault: 2 * time.Minute,
	PayoutHorizon: 24 * time.Hour, VerifyingNotice: 10 * time.Second, VerifyStuck: 2 * time.Minute,
	ModActionTimeout: 30 * time.Minute, GiveawayHorizon: 24 * time.Hour, RemindInterval: 5 * time.Minute,
	MaxOpen: 2 * time.Hour, WinnerCooldown: 5 * time.Second,
}

func ResolveWinners(p WinnerPolicy, entrants int, prize Prize) int {
	count := p.Count
	if p.Auto && entrants > 0 {
		count = (entrants-1)/Pins.AutoDivisor + 1
	}
	count = min(count, Pins.MaxWinners)
	if prize.Points > 0 {
		count = int(min(int64(count), prize.Points))
	}
	return max(1, count)
}
