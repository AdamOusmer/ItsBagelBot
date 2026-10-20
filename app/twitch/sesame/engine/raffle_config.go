// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"context"

	"ItsBagelBot/internal/projection"
	"ItsBagelBot/pkg/codec"
)

const RaffleModuleName = "raffle"

const (
	raffleDefaultPrize = int64(1000)
	maxRafflePrize     = int64(1_000_000)
)

// PrizePoints stays a Number: the dashboard saves "500" as a string, which an int64 field silently drops.
type RaffleModuleConfig struct {
	OpenedMessage   string       `json:"openedMessage"`
	JoinMessage     string       `json:"joinMessage"`
	AlreadyMessage  string       `json:"alreadyMessage"`
	NoRaffleMessage string       `json:"noRaffleMessage"`
	WonMessage      string       `json:"wonMessage"`
	ClaimOkMessage  string       `json:"claimOkMessage"`
	PrizePoints     codec.Number `json:"prizePoints"`
}

func (c RaffleModuleConfig) Prize() int64 {
	n, err := c.PrizePoints.Int64()
	if err != nil {
		return raffleDefaultPrize
	}
	return min(max(n, 0), maxRafflePrize)
}

func ReadRaffleConfig(ctx context.Context, proj projection.Reader, broadcasterID uint64) RaffleModuleConfig {
	var cfg RaffleModuleConfig
	view, _, _ := ModuleLookup{Proj: proj, BroadcasterID: broadcasterID, Name: RaffleModuleName, Absent: ModuleOff}.Resolve(ctx)
	if len(view.Configs) > 0 {
		_ = codec.Unmarshal(view.Configs, &cfg)
	}
	return cfg
}
