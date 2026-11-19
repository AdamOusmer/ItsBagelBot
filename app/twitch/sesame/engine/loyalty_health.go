// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"context"
	"time"

	pkg_valkey "ItsBagelBot/pkg/valkey"
)

type AccrualState string

const (
	AccrualHealthy  AccrualState = "healthy"
	AccrualStarting AccrualState = "starting"
	AccrualOffline  AccrualState = "offline"
	AccrualStalled  AccrualState = "stalled"
)

func (s *ValkeyLoyaltyClock) AccrualHealth(ctx context.Context, broadcasterID uint64) AccrualState {
	live, err := s.live.IsLive(ctx, broadcasterID)
	if err != nil {
		return AccrualStalled
	}
	if !live {
		return AccrualOffline
	}
	_, state, _ := (ModuleLookup{Proj: s.proj, BroadcasterID: broadcasterID, Name: LoyaltyModuleName, Absent: ModuleOff}).Resolve(ctx)
	if state != ModuleOn {
		return AccrualStalled
	}
	window, err := s.windowHealth(ctx, broadcasterID)
	if err != nil {
		return AccrualStalled
	}
	health := windowAccrualHealth(s.now(), window)
	if health == AccrualStalled {
		return health
	}
	status, err := s.watchStatus(ctx)
	if err != nil || !status.Healthy {
		return AccrualStalled
	}
	return health
}

func windowAccrualHealth(now time.Time, window pkg_valkey.WindowHealth) AccrualState {
	if !window.Active || window.Failed {
		return AccrualStalled
	}
	if window.LastPageAt == 0 {
		return firstPageHealth(time.UnixMilli(window.DueAt).Sub(now))
	}
	age := now.Sub(time.UnixMilli(window.LastPageAt))
	if age >= 0 && age <= 2*watchTickInterval {
		return AccrualHealthy
	}
	return AccrualStalled
}

func firstPageHealth(untilDue time.Duration) AccrualState {
	if untilDue > 0 && untilDue <= watchTickInterval+watchTickJitter {
		return AccrualStarting
	}
	return AccrualStalled
}
