// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package rpc

import (
	"context"

	"ItsBagelBot/app/db/loyalty/repository"
	loyaltyrpc "ItsBagelBot/internal/domain/rpc/loyalty"
)

func (l *loyaltyRPC) handleBalanceWager(ctx context.Context, req loyaltyrpc.Request) loyaltyrpc.Reply {
	userID, viewerID, ok, reply := parseIDs(req, false)
	if !ok {
		return reply
	}
	outcome, err := l.repo.BalanceWager(ctx, repository.Wager{UserID: userID, ViewerID: viewerID, Amount: req.Value, Won: req.Won})
	if err != nil {
		return l.fail("loyalty balance.wager", err)
	}
	reply = loyaltyrpc.Reply{Found: outcome.Found, Spent: outcome.Applied, LimitExceeded: outcome.LimitExceeded}
	if outcome.Balance != nil {
		reply.Balance = balanceView(outcome.Balance)
	}
	return reply
}
