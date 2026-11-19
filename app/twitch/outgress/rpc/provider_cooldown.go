// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package rpc

import (
	"context"
	"time"

	"ItsBagelBot/app/twitch/outgress/internal/twitch"
)

type ProviderCooldown struct {
	RetryAt      func(context.Context, string) (time.Time, error)
	ObserveReset func(context.Context, string, time.Time) error
}

type providerIdentity string

const appProviderIdentity providerIdentity = "helix:app"

func botProviderIdentity(botID string) providerIdentity {
	return providerIdentity("helix:bot:" + botID)
}

func (p ProviderCooldown) providerReady(ctx context.Context, identity providerIdentity, now func() time.Time) error {
	if p.RetryAt == nil {
		return nil
	}
	reset, err := p.RetryAt(ctx, string(identity))
	if err != nil {
		return admissionUnavailable(now())
	}
	// Server TIME decides whether a cooldown is live, never this pod's clock.
	if !reset.IsZero() {
		return &twitch.AdmissionError{Code: "rate_limited", RetryAt: reset}
	}
	return nil
}

func (p ProviderCooldown) recordProviderReset(ctx context.Context, identity providerIdentity, reset time.Time, now func() time.Time) error {
	if p.ObserveReset == nil {
		return nil
	}
	persistCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), time.Second)
	defer cancel()
	if err := p.ObserveReset(persistCtx, string(identity), reset); err != nil {
		return admissionUnavailable(now())
	}
	return nil
}

func admissionUnavailable(now time.Time) *twitch.AdmissionError {
	return &twitch.AdmissionError{Code: "unavailable", RetryAt: now.Add(time.Second)}
}
