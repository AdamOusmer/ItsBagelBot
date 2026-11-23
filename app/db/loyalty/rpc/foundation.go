// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package rpc

import (
	"context"
	"errors"

	"ItsBagelBot/app/db/loyalty/ent"
	"ItsBagelBot/app/db/loyalty/repository"
	domainrpc "ItsBagelBot/internal/domain/rpc"
	loyaltyrpc "ItsBagelBot/internal/domain/rpc/loyalty"

	"go.uber.org/zap"
)

type balanceMutationFn func(context.Context, repository.BalanceMutation) (repository.BalanceMutationResult, error)

func (l *loyaltyRPC) keyedBalanceHandler(verb string, mutate balanceMutationFn) func(context.Context, loyaltyrpc.Request) loyaltyrpc.Reply {
	targetRequired := "invalid viewer_id"
	if verb == loyaltyrpc.VerbBalanceDebit {
		targetRequired = loyaltyrpc.CounterTargetRequired
	}
	return func(ctx context.Context, req loyaltyrpc.Request) loyaltyrpc.Reply {
		userID, viewerID, ok, reply := parseIDs(req, false)
		if !ok {
			return reply
		}
		if viewerID == 0 {
			return refuse(domainrpc.CodeInvalid, targetRequired)
		}
		if req.AccountCreatedAt == nil {
			return refuse(domainrpc.CodeInvalid, "account_created_at is required")
		}
		result, err := mutate(ctx, repository.BalanceMutation{UserID: userID, ViewerID: viewerID,
			ViewerLogin: req.ViewerLogin, Value: req.Value, Key: req.Key, AccountCreatedAt: *req.AccountCreatedAt})
		if err != nil {
			return l.keyedBalanceFailure(verb, err)
		}
		return loyaltyrpc.Reply{Balance: result.Balance, Found: result.Balance != nil, Spent: result.Spent, Replayed: result.Replayed}
	}
}

func (l *loyaltyRPC) keyedBalanceFailure(verb string, err error) loyaltyrpc.Reply {
	refusal := domainrpc.Fail(err,
		domainrpc.Is(repository.ErrInvalidInput, domainrpc.CodeInvalid),
		domainrpc.Is(repository.ErrCreditKeyMismatch, domainrpc.CodeConflict),
		domainrpc.Is(repository.ErrCreditAccountMismatch, domainrpc.CodeForbidden),
		domainrpc.Is(repository.ErrCreditAccountRetired, domainrpc.CodeForbidden),
	)
	switch refusal.Code {
	case domainrpc.CodeInvalid, domainrpc.CodeConflict, domainrpc.CodeForbidden:
		return loyaltyrpc.Reply{Refusal: refusal}
	}
	l.log.Warn(verb+" failed", zap.Error(err))
	if verb == loyaltyrpc.VerbBalanceCredit {
		return refuse(domainrpc.CodeUnavailable, "loyalty credit unavailable")
	}
	return refuse(domainrpc.CodeUnavailable, "loyalty debit unavailable")
}

func (l *loyaltyRPC) snapshotUnavailable(err error) loyaltyrpc.BalanceSnapshot {
	if errors.Is(err, repository.ErrSnapshotGone) {
		return loyaltyrpc.BalanceSnapshot{Refusal: domainrpc.Refused(loyaltyrpc.RefusalSnapshotGone, err.Error())}
	}
	l.log.Warn("balance.snapshot failed", zap.Error(err))
	return loyaltyrpc.BalanceSnapshot{Refusal: domainrpc.Refused(domainrpc.CodeUnavailable, "loyalty snapshot unavailable")}
}

func (l *loyaltyRPC) handleBalanceSnapshot(ctx context.Context, req loyaltyrpc.BalanceSnapshotRequest) loyaltyrpc.BalanceSnapshot {
	userID, viewerID, ok, reply := requireViewer(loyaltyrpc.Request{UserID: req.UserID, ViewerID: req.ViewerID})
	if !ok {
		return loyaltyrpc.BalanceSnapshot{Refusal: reply.Refusal}
	}
	row, found, err := l.snapshotBalance(ctx, userID, viewerID, repository.SnapshotID(req.SnapshotID))
	if err != nil {
		return l.snapshotUnavailable(err)
	}
	if !found {
		return loyaltyrpc.BalanceSnapshot{}
	}
	return loyaltyrpc.BalanceSnapshot{Points: row.Points, WatchSeconds: row.WatchSeconds, Found: true}
}

func (l *loyaltyRPC) snapshotBalance(ctx context.Context, userID, viewerID uint64, id repository.SnapshotID) (*ent.Balance, bool, error) {
	if id == "" {
		return l.repo.BalanceGet(ctx, userID, viewerID)
	}
	return l.repo.SnapshotBalance(ctx, userID, viewerID, id)
}

func (l *loyaltyRPC) handleWatchStatus(ctx context.Context, _ struct{}) loyaltyrpc.WatchStatus {
	status, err := l.watchStatus(ctx)
	if err != nil {
		l.log.Warn("watch.status failed", zap.Error(err))
		return loyaltyrpc.WatchStatus{Refusal: domainrpc.Refused(domainrpc.CodeUnavailable, "watch outbox status unavailable")}
	}
	return loyaltyrpc.WatchStatus{OldestPendingAgeMS: status.OldestPendingAge.Milliseconds(), Healthy: status.Healthy}
}
