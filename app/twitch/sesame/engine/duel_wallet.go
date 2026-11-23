// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"context"
	"fmt"

	"ItsBagelBot/internal/analytics"
	catalog "ItsBagelBot/internal/domain/analytics"
)

type LoyaltyWallet struct {
	analytics *analytics.Recorder
	loyalty   LoyaltyStore
}

func NewLoyaltyWallet(loyalty LoyaltyStore) *LoyaltyWallet { return &LoyaltyWallet{loyalty: loyalty} }

func (w *LoyaltyWallet) Debit(ctx context.Context, broadcasterID uint64, entry DuelStake) (bool, bool, error) {
	out, err := w.loyalty.BalanceWager(ctx, PointWager{BroadcasterID: broadcasterID, ViewerID: entry.ViewerID, Login: entry.Login, Amount: entry.Stake})
	if err == nil && out.Applied {
		w.analytics.AddContext(ctx, catalog.PointsFlows, 1, "duel_stake")
	}
	return out.Found, out.Applied, err
}

func (w *LoyaltyWallet) Credit(ctx context.Context, broadcasterID uint64, entry DuelStake) (int64, error) {
	if entry.Stake <= 0 {
		return 0, nil
	}
	bal, found, err := w.loyalty.BalanceAdjustViewer(ctx, BalanceAdjustment{BroadcasterID: broadcasterID, ViewerID: entry.ViewerID, ViewerLogin: entry.Login, Value: entry.Stake})
	if err != nil {
		return 0, err
	}
	if !found {
		return 0, fmt.Errorf("duel: credit target %q unseen by loyalty (bal %+v)", entry.Login, bal)
	}
	return bal.Points, nil
}
