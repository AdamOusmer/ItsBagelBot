// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package rpc

import (
	"context"

	"ItsBagelBot/app/db/loyalty/repository"
	loyaltyrpc "ItsBagelBot/internal/domain/rpc/loyalty"
)

func (l *loyaltyRPC) handleBalanceSpend(ctx context.Context, req loyaltyrpc.Request) loyaltyrpc.Reply {
	userID, viewerID, ok, reply := requireViewer(req)
	if !ok {
		return reply
	}
	row, found, spent, err := l.repo.BalanceSpendViewer(ctx, repository.Spend{UserID: userID, ViewerID: viewerID, ViewerLogin: req.ViewerLogin, Amount: req.Value})
	if err != nil {
		return l.fail("loyalty balance.spend", err)
	}
	if !found {
		return loyaltyrpc.Reply{Found: false}
	}
	return loyaltyrpc.Reply{Balance: balanceView(row), Found: true, Spent: spent}
}
