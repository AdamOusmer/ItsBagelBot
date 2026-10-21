// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"context"
	"time"

	"ItsBagelBot/internal/projection"
	"ItsBagelBot/pkg/codec"
)

const RaffleModuleName = "raffle"

const (
	raffleDefaultPrize = int64(1000)
	MaxRafflePrize     = int64(1_000_000)

	raffleDefaultDuration = time.Minute
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
	DurationSeconds codec.Number `json:"durationSeconds"`

	OpenedEnabled   string `json:"openedEnabled"`
	JoinEnabled     string `json:"joinEnabled"`
	AlreadyEnabled  string `json:"alreadyEnabled"`
	NoRaffleEnabled string `json:"noRaffleEnabled"`
	WonEnabled      string `json:"wonEnabled"`
	ClaimOkEnabled  string `json:"claimOkEnabled"`
	RemindEnabled   string `json:"remindEnabled"`
	ClaimEnabled    string `json:"claimEnabled"`
	ClaimPrize      string `json:"claimPrize"`
}

func lineDefaultOn(v string) bool { return v != "off" && v != "false" }

func lineDefaultOff(v string) bool { return v == "on" || v == "true" }

func (c RaffleModuleConfig) OpenedOn() bool   { return lineDefaultOn(c.OpenedEnabled) }
func (c RaffleModuleConfig) JoinOn() bool     { return lineDefaultOff(c.JoinEnabled) }
func (c RaffleModuleConfig) AlreadyOn() bool  { return lineDefaultOff(c.AlreadyEnabled) }
func (c RaffleModuleConfig) NoRaffleOn() bool { return lineDefaultOn(c.NoRaffleEnabled) }
func (c RaffleModuleConfig) WonOn() bool      { return lineDefaultOn(c.WonEnabled) }
func (c RaffleModuleConfig) ClaimOkOn() bool  { return lineDefaultOn(c.ClaimOkEnabled) }
func (c RaffleModuleConfig) RemindOn() bool   { return lineDefaultOn(c.RemindEnabled) }
func (c RaffleModuleConfig) ClaimOn() bool    { return lineDefaultOff(c.ClaimEnabled) }

func (c RaffleModuleConfig) Prize() int64 {
	n, err := c.PrizePoints.Int64()
	if err != nil {
		return raffleDefaultPrize
	}
	return min(max(n, 0), MaxRafflePrize)
}

func (c RaffleModuleConfig) Duration() time.Duration {
	n, err := c.DurationSeconds.Int64()
	if err != nil || n <= 0 {
		return raffleDefaultDuration
	}
	return ClampRaffleDuration(time.Duration(min(n, int64(maxRaffleDuration/time.Second))) * time.Second)
}

func ReadRaffleConfig(ctx context.Context, proj projection.Reader, broadcasterID uint64) RaffleModuleConfig {
	var cfg RaffleModuleConfig
	view, _, _ := ModuleLookup{Proj: proj, BroadcasterID: broadcasterID, Name: RaffleModuleName, Absent: ModuleOff}.Resolve(ctx)
	if len(view.Configs) > 0 {
		_ = codec.Unmarshal(view.Configs, &cfg)
	}
	return cfg
}
