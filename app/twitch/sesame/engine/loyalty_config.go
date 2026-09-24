// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"context"

	"ItsBagelBot/internal/projection"
	"ItsBagelBot/pkg/codec"
)

const LoyaltyModuleName = "loyalty"

const (
	defaultPointsName         = "points"
	defaultSubPoints          = 500
	defaultResubPoints        = 500
	defaultGiftSubPoints      = 100
	defaultCheerPointsPer100  = 50
	defaultWatchPointsPerTick = 10
)

type LoyaltyModuleConfig struct {
	PointsName         string `json:"pointsName"`
	SubPoints          int64  `json:"subPoints"`
	ResubPoints        int64  `json:"resubPoints"`
	GiftSubPoints      int64  `json:"giftSubPoints"`
	CheerPointsPer100  int64  `json:"cheerPointsPer100"`
	WatchPointsPerTick int64  `json:"watchPointsPerTick"`
	ModSetPoints       int    `json:"modSetPoints"`
	ModAdjustPoints    int    `json:"modAdjustPoints"`
	ViewerTransfers    int    `json:"viewerTransfers"`
}

const maxRate = int64(1_000_000_000)

func rate(v, def int64) int64 {
	switch {
	case v == 0:
		return def
	case v < 0:
		return 0
	case v > maxRate:
		return maxRate
	default:
		return v
	}
}

func (c LoyaltyModuleConfig) Name() string {
	if c.PointsName == "" {
		return defaultPointsName
	}
	return c.PointsName
}

func (c LoyaltyModuleConfig) EffectiveSubPoints() int64 { return rate(c.SubPoints, defaultSubPoints) }
func (c LoyaltyModuleConfig) EffectiveResubPoints() int64 {
	return rate(c.ResubPoints, defaultResubPoints)
}
func (c LoyaltyModuleConfig) EffectiveGiftSubPoints() int64 {
	return rate(c.GiftSubPoints, defaultGiftSubPoints)
}
func (c LoyaltyModuleConfig) EffectiveCheerPointsPer100() int64 {
	return rate(c.CheerPointsPer100, defaultCheerPointsPer100)
}
func (c LoyaltyModuleConfig) EffectiveWatchPointsPerTick() int64 {
	return rate(c.WatchPointsPerTick, defaultWatchPointsPerTick)
}

func capabilityOn(v int) bool { return v >= 0 }

func (c LoyaltyModuleConfig) ModsMaySetPoints() bool    { return capabilityOn(c.ModSetPoints) }
func (c LoyaltyModuleConfig) ModsMayAdjustPoints() bool { return capabilityOn(c.ModAdjustPoints) }
func (c LoyaltyModuleConfig) ViewersMayTransfer() bool  { return capabilityOn(c.ViewerTransfers) }

func TierMultiplier(tier string) int64 {
	switch tier {
	case "2000":
		return 2
	case "3000":
		return 6
	default:
		return 1
	}
}

func ReadLoyaltyConfig(ctx context.Context, proj projection.Reader, broadcasterID uint64) (LoyaltyModuleConfig, bool) {
	return loyaltyModuleConfig(ctx, proj, broadcasterID)
}

func loyaltyModuleConfig(ctx context.Context, proj projection.Reader, broadcasterID uint64) (LoyaltyModuleConfig, bool) {
	view, state, _ := ModuleLookup{Proj: proj, BroadcasterID: broadcasterID, Name: LoyaltyModuleName, Absent: ModuleOff}.Resolve(ctx)
	if state != ModuleOn {
		return LoyaltyModuleConfig{}, false
	}
	var cfg LoyaltyModuleConfig
	if len(view.Configs) > 0 {
		if err := codec.Unmarshal(view.Configs, &cfg); err != nil {
			return LoyaltyModuleConfig{}, false
		}
	}
	return cfg, true
}
