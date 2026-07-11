package engine

import (
	"context"
	"encoding/json"

	"ItsBagelBot/internal/projection"
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
}

// rate applies the zero-default / negative-off convention.
func rate(v, def int64) int64 {
	switch {
	case v == 0:
		return def
	case v < 0:
		return 0
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
			_ = json.Unmarshal(v.Configs, &cfg)
		}
		return cfg, true
	}
	return LoyaltyModuleConfig{}, false
}
