// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package rpc

import (
	"context"

	"ItsBagelBot/app/db/loyalty/repository"
	domainrpc "ItsBagelBot/internal/domain/rpc"
	loyaltyrpc "ItsBagelBot/internal/domain/rpc/loyalty"
)

func (l *loyaltyRPC) handleBalanceSpend(ctx context.Context, req loyaltyrpc.Request) loyaltyrpc.Reply {
	userID, viewerID, ok, reply := parseIDs(req, false)
	if !ok {
		return reply
	}
	if viewerID == 0 {
		return refuse(domainrpc.CodeInvalid, "invalid viewer_id")
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
