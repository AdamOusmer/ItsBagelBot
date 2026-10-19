// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package rpc

import (
	"context"

	"ItsBagelBot/app/db/loyalty/ent"
	"ItsBagelBot/app/db/loyalty/repository"
	loyaltyrpc "ItsBagelBot/internal/domain/rpc/loyalty"
)

func (l *loyaltyRPC) handleBalanceSpend(ctx context.Context, req loyaltyrpc.Request) loyaltyrpc.Reply {
	userID, viewerID, ok, reply := parseIDs(req, false)
	if !ok {
		return reply
	}
	row, found, spent, err := l.spendBalance(ctx, repository.Spend{UserID: userID, ViewerID: viewerID, ViewerLogin: req.ViewerLogin, Amount: req.Value})
	if err != nil {
		return l.fail("loyalty balance.spend", err)
	}
	if !found {
		return loyaltyrpc.Reply{Found: false}
	}
	return loyaltyrpc.Reply{Balance: balanceView(row), Found: true, Spent: spent}
}

func (l *loyaltyRPC) spendBalance(ctx context.Context, spend repository.Spend) (*ent.Balance, bool, bool, error) {
	if spend.ViewerID != 0 {
		return l.repo.BalanceSpendViewer(ctx, spend)
	}
	return l.repo.BalanceSpend(ctx, spend.UserID, spend.ViewerLogin, spend.Amount)
}
