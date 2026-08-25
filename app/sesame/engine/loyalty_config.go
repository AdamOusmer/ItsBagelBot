// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"context"

	"ItsBagelBot/internal/projection"
	"ItsBagelBot/pkg/codec"
)

// LoyaltyModuleName is the ModuleView key the dashboard's (future) Loyalty tab
// writes: its enable toggle gates the module and the watch tick, and its
// Configs blob carries the point rates below.
const LoyaltyModuleName = "loyalty"

// Default point rates. A freshly enabled module with an empty blob runs on
// these; the blob only carries what the broadcaster changed.
const (
	defaultPointsName         = "points"
	defaultSubPoints          = 500
	defaultResubPoints        = 500
	defaultGiftSubPoints      = 100
	defaultCheerPointsPer100  = 50
	defaultWatchPointsPerTick = 10
)

// LoyaltyModuleConfig is the "loyalty" module's Configs blob. Every rate uses
// zero-means-default so an empty blob is fully functional; a negative value
// switches that source off (the dashboard will write -1 for a disabled
// toggle).
type LoyaltyModuleConfig struct {
	// PointsName is the currency's chat-facing name ("points", "bagels", …).
	PointsName string `json:"pointsName"`
	// SubPoints per new subscription (gift recipients included), scaled by the
	// tier multiplier.
	SubPoints int64 `json:"subPoints"`
	// ResubPoints per resubscribe share, scaled by the tier multiplier.
	ResubPoints int64 `json:"resubPoints"`
	// GiftSubPoints per gifted sub, credited to the gifter.
	GiftSubPoints int64 `json:"giftSubPoints"`
	// CheerPointsPer100 per 100 bits cheered (pro-rated per bit).
	CheerPointsPer100 int64 `json:"cheerPointsPer100"`
	// WatchPointsPerTick per watch tick (see watchTickInterval) while live.
	WatchPointsPerTick int64 `json:"watchPointsPerTick"`
	// Granular chat permissions over the mutating loyalty verbs, so a
	// broadcaster can hand moderators exactly as much power as they want.
	// Zero keeps the historical default (the capability is on — every mod
	// could already set/add points before this field existed); a negative
	// value switches it off, mirroring the rates' negative-means-off
	// convention. The broadcaster's own commands are never gated by these.
	// ModSetPoints gates "!points set <user> <n>" (absolute writes).
	ModSetPoints int `json:"modSetPoints"`
	// ModAdjustPoints gates "!points add|remove <user> <±n>" (delta writes).
	ModAdjustPoints int `json:"modAdjustPoints"`
	// ViewerTransfers gates "!points give @user <n>" — everyone's verb for
	// moving their OWN points, mods included.
	ViewerTransfers int `json:"viewerTransfers"`
}

// maxRate caps any configured points rate. Downstream math multiplies rates
// by per-event quantities (bits per cheer, months per sub); a broadcaster-set
// config near MaxInt64 wraps that product negative — self-inflicted negative
// balances, but still an int64 overflow class we can delete outright. 1e9
// points per event is past any sane economy.
const maxRate = int64(1_000_000_000)

// rate applies the zero-default / negative-off convention, plus the ceiling.
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

// capabilityOn applies the toggle convention: zero (or any positive value,
// for a dashboard that writes 1) means on, a negative means off.
func capabilityOn(v int) bool { return v >= 0 }

func (c LoyaltyModuleConfig) ModsMaySetPoints() bool    { return capabilityOn(c.ModSetPoints) }
func (c LoyaltyModuleConfig) ModsMayAdjustPoints() bool { return capabilityOn(c.ModAdjustPoints) }
func (c LoyaltyModuleConfig) ViewersMayTransfer() bool  { return capabilityOn(c.ViewerTransfers) }

// TierMultiplier scales sub/resub points by the EventSub tier ("1000",
// "2000", "3000"), mirroring the going rate of a tier's price.
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

// ReadLoyaltyConfig resolves a broadcaster's "loyalty" ModuleView, reporting
// false when the module is missing, disabled or unreadable. An enabled module
// with an empty blob returns the zero config (all defaults). Wager games
// call this so they share the currency name and stay inert while loyalty is
// off, instead of carrying a second copy of the name and running against a
// ledger nobody is earning on.
func ReadLoyaltyConfig(ctx context.Context, proj projection.Reader, broadcasterID uint64) (LoyaltyModuleConfig, bool) {
	return loyaltyModuleConfig(ctx, proj, broadcasterID)
}

// loyaltyModuleConfig resolves a broadcaster's "loyalty" ModuleView, reporting
// false when the module is missing, disabled or unreadable. An enabled module
// with an empty blob returns the zero config (all defaults).
func loyaltyModuleConfig(ctx context.Context, proj projection.Reader, broadcasterID uint64) (LoyaltyModuleConfig, bool) {
	views, err := proj.Modules(ctx, broadcasterID)
	if err != nil {
		return LoyaltyModuleConfig{}, false
	}
	for _, v := range views {
		if v.Name != LoyaltyModuleName {
			continue
		}
		if !v.IsEnabled {
			return LoyaltyModuleConfig{}, false
		}
		var cfg LoyaltyModuleConfig
		if len(v.Configs) > 0 {
			_ = codec.Unmarshal(v.Configs, &cfg)
		}
		return cfg, true
	}
	return LoyaltyModuleConfig{}, false
}
