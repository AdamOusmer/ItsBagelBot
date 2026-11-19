// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package rpc

import (
	"context"

	"ItsBagelBot/app/db/loyalty/repository"
	domainrpc "ItsBagelBot/internal/domain/rpc"
	loyaltyrpc "ItsBagelBot/internal/domain/rpc/loyalty"

	"go.uber.org/zap"
)

func (l *loyaltyRPC) handleBalanceCredit(ctx context.Context, req loyaltyrpc.Request) loyaltyrpc.Reply {
	userID, viewerID, ok, reply := requireViewer(req)
	if !ok {
		return reply
	}
	if req.AccountCreatedAt == nil {
		return refuse(domainrpc.CodeInvalid, "account_created_at is required")
	}
	result, err := l.repo.BalanceCredit(ctx, repository.BalanceCredit{UserID: userID, ViewerID: viewerID,
		ViewerLogin: req.ViewerLogin, Value: req.Value, Key: req.Key, AccountCreatedAt: *req.AccountCreatedAt})
	if err != nil {
		return l.creditFailure(err)
	}
	return loyaltyrpc.Reply{Balance: result.Balance, Found: true, Replayed: result.Replayed}
}

func (l *loyaltyRPC) creditFailure(err error) loyaltyrpc.Reply {
	refusal := domainrpc.Fail(err,
		domainrpc.Is(repository.ErrInvalidInput, domainrpc.CodeInvalid),
		domainrpc.Is(repository.ErrCreditKeyMismatch, domainrpc.CodeConflict),
		domainrpc.Is(repository.ErrCreditAccountMismatch, domainrpc.CodeForbidden),
		domainrpc.Is(repository.ErrCreditAccountRetired, domainrpc.CodeForbidden),
	)
	switch refusal.Code {
	case domainrpc.CodeInvalid, domainrpc.CodeConflict, domainrpc.CodeForbidden:
		return loyaltyrpc.Reply{Refusal: refusal}
	default:
		l.log.Warn("balance.credit failed", zap.Error(err))
		return refuse(domainrpc.CodeUnavailable, "loyalty credit unavailable")
	}
}

func (l *loyaltyRPC) handleBalanceSnapshot(ctx context.Context, req loyaltyrpc.Request) loyaltyrpc.BalanceSnapshot {
	userID, viewerID, ok, reply := requireViewer(req)
	if !ok {
		return loyaltyrpc.BalanceSnapshot{Refusal: reply.Refusal}
	}
	route, err := l.resolveBoard(ctx, userID, loyaltyrpc.Request{})
	if err != nil {
		return loyaltyrpc.BalanceSnapshot{Refusal: l.fail(loyaltyrpc.VerbBalanceSnapshot, err).Refusal}
	}
	row, found, err := route.balance(ctx, viewerID)
	if err != nil {
		l.log.Warn("balance.snapshot failed", zap.Error(err))
		return loyaltyrpc.BalanceSnapshot{Refusal: domainrpc.Refused(domainrpc.CodeUnavailable, "loyalty snapshot unavailable")}
	}
	if !found {
		return loyaltyrpc.BalanceSnapshot{FreezeStamp: route.activeStamp()}
	}
	return loyaltyrpc.BalanceSnapshot{Points: row.Points, WatchSeconds: row.WatchSeconds, Found: true, FreezeStamp: route.activeStamp()}
}

func (l *loyaltyRPC) handleWatchStatus(ctx context.Context, _ struct{}) loyaltyrpc.WatchStatus {
	status, err := l.watchStatus(ctx)
	if err != nil {
		l.log.Warn("watch.status failed", zap.Error(err))
		return loyaltyrpc.WatchStatus{Refusal: domainrpc.Refused(domainrpc.CodeUnavailable, "watch outbox status unavailable")}
	}
	return loyaltyrpc.WatchStatus{OldestPendingAgeMS: status.OldestPendingAge.Milliseconds(), Healthy: status.Healthy}
}
