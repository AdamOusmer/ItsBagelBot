// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"context"

	"ItsBagelBot/internal/projection"
	pkg_valkey "ItsBagelBot/pkg/valkey"
)

type LegacyRaffleConfig struct {
	RaffleModuleConfig
	ClaimEnabled string `json:"claimEnabled"`
	ClaimPrize   string `json:"claimPrize"`
}

func (c LegacyRaffleConfig) ClaimOn() bool { return lineDefaultOff(c.ClaimEnabled) }

func ReadLegacyRaffleConfig(ctx context.Context, proj projection.Reader, broadcasterID uint64) LegacyRaffleConfig {
	return readRaffleConfig[LegacyRaffleConfig](ctx, proj, broadcasterID)
}

type RaffleClaim int

const (
	ClaimOk RaffleClaim = iota
	ClaimAlready
	ClaimLate
	ClaimNone
)

func (s *ValkeyRaffleStore) Claim(ctx context.Context, broadcasterID uint64, viewerID string) (RaffleClaim, error) {
	if viewerID == "" {
		return ClaimNone, nil
	}
	code, err := (pkg_valkey.OutcomeClaim{Key: raffleKey(raffleLastPrefix, broadcasterID)}).Claim(
		ctx, s.client, viewerID, raffleClaimWindow,
	)
	if err != nil {
		return ClaimNone, err
	}
	switch code {
	case pkg_valkey.OutcomeClaimOK:
		return ClaimOk, nil
	case pkg_valkey.OutcomeClaimAlready:
		return ClaimAlready, nil
	case pkg_valkey.OutcomeClaimLate:
		return ClaimLate, nil
	default:
		return ClaimNone, nil
	}
}
