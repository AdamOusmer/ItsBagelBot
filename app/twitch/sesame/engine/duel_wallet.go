// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"context"
	"fmt"

	"ItsBagelBot/internal/analytics"
	catalog "ItsBagelBot/internal/domain/analytics"
	loyaltyrpc "ItsBagelBot/internal/domain/rpc/loyalty"
)

// Debit must stay on balance.spend's conditional UPDATE so concurrent spends never go negative.
type LoyaltyWallet struct {
	analytics *analytics.Recorder
	loyalty   LoyaltyStore
}

func NewLoyaltyWallet(loyalty LoyaltyStore) *LoyaltyWallet { return &LoyaltyWallet{loyalty: loyalty} }

func (w *LoyaltyWallet) Debit(ctx context.Context, broadcasterID uint64, entry DuelStake) (bool, bool, error) {
	_, found, spent, err := w.loyalty.BalanceSpend(ctx, PointSpend{BroadcasterID: broadcasterID, ViewerID: entry.ViewerID, Login: entry.Login, Amount: entry.Stake})
	if err == nil && spent {
		w.analytics.AddContext(ctx, catalog.PointsFlows, 1, "duel_stake")
	}
	return found, spent, err
}

func (w *LoyaltyWallet) Credit(ctx context.Context, broadcasterID uint64, entry DuelStake) error {
	if entry.Stake <= 0 {
		return nil
	}
	bal, found, err := w.credit(ctx, broadcasterID, entry)
	if err != nil {
		return err
	}
	if !found {
		return fmt.Errorf("duel: credit target %q unseen by loyalty (bal %+v)", entry.Login, bal)
	}
	return nil
}

func (w *LoyaltyWallet) credit(ctx context.Context, broadcasterID uint64, entry DuelStake) (loyaltyrpc.Balance, bool, error) {
	return w.loyalty.BalanceAdjustViewer(ctx, BalanceAdjustment{BroadcasterID: broadcasterID, ViewerID: entry.ViewerID, ViewerLogin: entry.Login, Value: entry.Stake})
}
