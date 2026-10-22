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

	defaultWatchTier1Percent = 150
	defaultWatchTier2Percent = 200
	defaultWatchTier3Percent = 600
	minWatchTierPercent      = 100
	maxWatchTierPercent      = 1000
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
	StreamerPoints     int    `json:"streamerPoints"`
	WatchSubBonus      bool   `json:"watchSubBonus"`
	WatchTier1Percent  int64  `json:"watchTier1Percent"`
	WatchTier2Percent  int64  `json:"watchTier2Percent"`
	WatchTier3Percent  int64  `json:"watchTier3Percent"`
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

func (c LoyaltyModuleConfig) WatchPointsFor(tier string) int64 {
	base := c.EffectiveWatchPointsPerTick()
	if !c.WatchSubBonus {
		return base
	}
	return base * c.watchTierPercent(tier) / 100
}

func (c LoyaltyModuleConfig) watchTierPercent(tier string) int64 {
	switch tier {
	case "1000":
		return tierPercent(c.WatchTier1Percent, defaultWatchTier1Percent)
	case "2000":
		return tierPercent(c.WatchTier2Percent, defaultWatchTier2Percent)
	case "3000":
		return tierPercent(c.WatchTier3Percent, defaultWatchTier3Percent)
	default:
		return 100
	}
}

func tierPercent(v, def int64) int64 {
	if v == 0 {
		return def
	}
	return min(max(v, minWatchTierPercent), maxWatchTierPercent)
}

func capabilityOn(v int) bool { return v >= 0 }

func (c LoyaltyModuleConfig) ModsMaySetPoints() bool    { return capabilityOn(c.ModSetPoints) }
func (c LoyaltyModuleConfig) ModsMayAdjustPoints() bool { return capabilityOn(c.ModAdjustPoints) }
func (c LoyaltyModuleConfig) ViewersMayTransfer() bool  { return capabilityOn(c.ViewerTransfers) }

// AutomaticPoints applies the streamer's earning preference without restricting
// transfers, manual grants or watch time.
func (c LoyaltyModuleConfig) AutomaticPoints(broadcasterID, viewerID uint64, points int64) int64 {
	if broadcasterID == viewerID && !capabilityOn(c.StreamerPoints) {
		return 0
	}
	return points
}

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
