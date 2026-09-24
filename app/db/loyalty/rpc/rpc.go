// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package rpc

import (
	"context"
	"errors"
	"strconv"

	"go.uber.org/zap"

	"ItsBagelBot/app/db/loyalty/ent"
	"ItsBagelBot/app/db/loyalty/repository"
	domainrpc "ItsBagelBot/internal/domain/rpc"
	loyaltyrpc "ItsBagelBot/internal/domain/rpc/loyalty"
	"ItsBagelBot/pkg/bus"
)

type loyaltyRPC struct {
	repo *repository.Loyalty
	log  *zap.Logger
}

func Subscribe(w Wiring, prefix string) error {
	l := &loyaltyRPC{repo: w.Repo, log: w.Log}

	return bus.ServeVerbs(w.RPCWiring, prefix,
		bus.At("balance.get", l.handleBalanceGet),
		bus.At("balance.set", l.handleBalanceSet),
		bus.At("balance.add", l.handleBalanceAdd),
		bus.At("balance.spend", l.handleBalanceSpend),
		bus.At("balance.transfer", l.handleBalanceTransfer),
		bus.At("top.get", l.handleTopGet),
		bus.At("counter.get", l.handleCounterGet),
		bus.At("counter.create", l.handleCounterCreate),
		bus.At("counter.set", l.handleCounterSet),
		bus.At("counter.rename", l.handleCounterRename),
		bus.At("counter.delete", l.handleCounterDelete),
		bus.At("counter.entry.delete", l.handleCounterEntryDelete),
		bus.At("counter.list", l.handleCounterList),
		bus.At("counter.board", l.handleCounterBoard),
		bus.At("counter.entries", l.handleCounterEntries),
		bus.At("counter.trial", l.handleCounterTrial),
		bus.At("counter.promote_trial", l.handlePromoteTrial),
	)
}

type Wiring struct {
	bus.RPCWiring
	Repo *repository.Loyalty
}

func refuse(code domainrpc.Code, message string) loyaltyrpc.Reply {
	return loyaltyrpc.Reply{Refusal: domainrpc.Refused(code, message)}
}

func parseIDs(req loyaltyrpc.Request, allowBotNS bool) (userID, viewerID uint64, ok bool, reply loyaltyrpc.Reply) {
	uid, err := bus.UserID(req.UserID)
	if err != nil {
		return 0, 0, false, refuse(domainrpc.CodeInvalid, err.Error())
	}
	if uid == 0 && !allowBotNS {
		return 0, 0, false, refuse(domainrpc.CodeInvalid, bus.ErrInvalidUserID.Error())
	}
	if req.ViewerID != "" {
		vid, err := strconv.ParseUint(req.ViewerID, 10, 64)
		if err != nil {
			return 0, 0, false, refuse(domainrpc.CodeInvalid, "invalid viewer_id")
		}
		viewerID = vid
	}
	return uid, viewerID, true, loyaltyrpc.Reply{}
}

func (l *loyaltyRPC) fail(op string, err error) loyaltyrpc.Reply {
	if errors.Is(err, repository.ErrInvalidInput) {
		return refuse(domainrpc.CodeInvalid, err.Error())
	}
	l.log.Warn(op+" failed", zap.Error(err))
	return refuse(domainrpc.CodeInternal, "loyalty request failed")
}

func balanceView(row *ent.Balance) *loyaltyrpc.Balance {
	return &loyaltyrpc.Balance{
		ViewerID:     strconv.FormatUint(row.ViewerID, 10),
		ViewerLogin:  row.ViewerLogin,
		ViewerName:   row.ViewerName,
		Points:       row.Points,
		WatchSeconds: row.WatchSeconds,
	}
}

func (l *loyaltyRPC) handleBalanceGet(ctx context.Context, req loyaltyrpc.Request) loyaltyrpc.Reply {
	userID, viewerID, ok, reply := parseIDs(req, false)
	if !ok {
		return reply
	}
	if viewerID == 0 {
		return refuse(domainrpc.CodeInvalid, "invalid viewer_id")
	}
	row, found, err := l.repo.BalanceGet(ctx, userID, viewerID)
	if err != nil {
		return l.fail("loyalty balance.get", err)
	}
	if !found {
		return loyaltyrpc.Reply{Balance: &loyaltyrpc.Balance{ViewerID: req.ViewerID}, Found: false}
	}
	return loyaltyrpc.Reply{Balance: balanceView(row), Found: true}
}

func (l *loyaltyRPC) handleBalanceSet(ctx context.Context, req loyaltyrpc.Request) loyaltyrpc.Reply {
	return l.adjustBalance(ctx, req, true)
}

func (l *loyaltyRPC) handleBalanceAdd(ctx context.Context, req loyaltyrpc.Request) loyaltyrpc.Reply {
	return l.adjustBalance(ctx, req, false)
}

func (l *loyaltyRPC) handleBalanceSpend(ctx context.Context, req loyaltyrpc.Request) loyaltyrpc.Reply {
	userID, _, ok, reply := parseIDs(req, false)
	if !ok {
		return reply
	}
	row, found, spent, err := l.repo.BalanceSpend(ctx, userID, req.ViewerLogin, req.Value)
	if err != nil {
		return l.fail("loyalty balance.spend", err)
	}
	if !found {
		return loyaltyrpc.Reply{Found: false}
	}
	return loyaltyrpc.Reply{Balance: balanceView(row), Found: true, Spent: spent}
}

func (l *loyaltyRPC) adjustBalance(ctx context.Context, req loyaltyrpc.Request, absolute bool) loyaltyrpc.Reply {
	userID, _, ok, reply := parseIDs(req, false)
	if !ok {
		return reply
	}
	row, found, err := l.repo.BalanceAdjust(ctx, userID, req.ViewerLogin, req.Value, absolute)
	if err != nil {
		return l.fail("loyalty balance adjust", err)
	}
	if !found {
		return loyaltyrpc.Reply{Found: false}
	}
	return loyaltyrpc.Reply{Balance: balanceView(row), Found: true}
}

func (l *loyaltyRPC) handleBalanceTransfer(ctx context.Context, req loyaltyrpc.Request) loyaltyrpc.Reply {
	userID, viewerID, ok, reply := parseIDs(req, false)
	if !ok {
		return reply
	}
	if viewerID == 0 {
		return refuse(domainrpc.CodeInvalid, "invalid viewer_id")
	}
	out, found, err := l.repo.BalanceTransfer(ctx, repository.Transfer{UserID: userID, FromViewerID: viewerID, TargetLogin: req.ViewerLogin, Amount: req.Value})
	if err != nil {
		return l.fail("loyalty balance.transfer", err)
	}
	if !found || out == nil {
		return loyaltyrpc.Reply{Found: false}
	}
	sent := loyaltyrpc.Reply{Balance: balanceView(out.From)}
	sent.Found = true
	sent.Spent = out.To != nil
	if out.To != nil {
		sent.TargetBalance = balanceView(out.To)
	}
	return sent
}

func (l *loyaltyRPC) handleCounterEntries(ctx context.Context, req loyaltyrpc.Request) loyaltyrpc.Reply {
	userID, _, ok, reply := parseIDs(req, true)
	if !ok {
		return reply
	}
	rows, logins, err := l.repo.CounterEntries(ctx, userID, req.Name, req.Limit)
	if err != nil {
		return l.fail("loyalty counter.entries", err)
	}
	entries := make([]loyaltyrpc.CounterEntry, 0, len(rows))
	for _, e := range rows {
		login := e.ViewerLogin
		if login == "" {
			login = logins[e.ViewerID]
		}
		entries = append(entries, loyaltyrpc.CounterEntry{
			ViewerID:    strconv.FormatUint(e.ViewerID, 10),
			ViewerLogin: login,
			ViewerName:  e.ViewerName,
			Command:     e.Command,
			Value:       e.Value,
		})
	}
	return loyaltyrpc.Reply{Entries: entries, Found: true}
}

func (l *loyaltyRPC) handleTopGet(ctx context.Context, req loyaltyrpc.Request) loyaltyrpc.Reply {
	userID, _, ok, reply := parseIDs(req, false)
	if !ok {
		return reply
	}
	rows, err := l.repo.Top(ctx, userID, req.Limit)
	if err != nil {
		return l.fail("loyalty top.get", err)
	}
	top := make([]loyaltyrpc.Balance, 0, len(rows))
	for _, row := range rows {
		top = append(top, *balanceView(row))
	}
	return loyaltyrpc.Reply{Top: top, Found: true}
}

func (l *loyaltyRPC) handleCounterGet(ctx context.Context, req loyaltyrpc.Request) loyaltyrpc.Reply {
	userID, viewerID, ok, reply := parseIDs(req, true)
	if !ok {
		return reply
	}
	row, value, found, err := l.repo.CounterGet(ctx, userID, req.Name, viewerID, req.Command)
	if err != nil {
		return l.fail("loyalty counter.get", err)
	}
	if !found {
		return loyaltyrpc.Reply{Found: false}
	}
	return loyaltyrpc.Reply{
		Counter: &loyaltyrpc.Counter{Name: row.Name, Scope: row.Scope, Value: value},
		Found:   true,
	}
}

func (l *loyaltyRPC) handleCounterTrial(ctx context.Context, req loyaltyrpc.Request) loyaltyrpc.Reply {
	return l.counterRows(ctx, req, false, "loyalty counter.trial", l.repo.TrialCounters)
}

func (l *loyaltyRPC) handlePromoteTrial(ctx context.Context, req loyaltyrpc.Request) loyaltyrpc.Reply {
	userID, _, ok, reply := parseIDs(req, false)
	if !ok {
		return reply
	}
	promoted, err := l.repo.PromoteTrial(ctx, userID)
	if err != nil {
		return l.fail("loyalty counter.promote_trial", err)
	}
	return loyaltyrpc.Reply{Found: promoted}
}

func (l *loyaltyRPC) handleCounterCreate(ctx context.Context, req loyaltyrpc.Request) loyaltyrpc.Reply {
	userID, _, ok, reply := parseIDs(req, true)
	if !ok {
		return reply
	}
	row, err := l.repo.CounterCreate(ctx, userID, req.Name, req.Scope)
	if err != nil {
		return l.fail("loyalty counter.create", err)
	}
	return loyaltyrpc.Reply{
		Counter: &loyaltyrpc.Counter{Name: row.Name, Scope: row.Scope, Value: row.Value},
		Found:   true,
	}
}

func (l *loyaltyRPC) foundReply(op string, found bool, err error) loyaltyrpc.Reply {
	if err != nil {
		return l.fail(op, err)
	}
	return loyaltyrpc.Reply{Found: found}
}

func (l *loyaltyRPC) handleCounterSet(ctx context.Context, req loyaltyrpc.Request) loyaltyrpc.Reply {
	userID, viewerID, ok, reply := parseIDs(req, true)
	if !ok {
		return reply
	}
	target := repository.SetTarget{ViewerID: viewerID, Command: req.Command, ViewerLogin: req.ViewerLogin}
	found, err := l.repo.CounterSet(ctx, userID, req.Name, target, req.Value)
	return l.foundReply("loyalty counter.set", found, err)
}

func (l *loyaltyRPC) handleCounterRename(ctx context.Context, req loyaltyrpc.Request) loyaltyrpc.Reply {
	userID, _, ok, reply := parseIDs(req, true)
	if !ok {
		return reply
	}
	found, err := l.repo.CounterRename(ctx, userID, req.Name, req.NewName)
	return l.foundReply("loyalty counter.rename", found, err)
}

func (l *loyaltyRPC) handleCounterDelete(ctx context.Context, req loyaltyrpc.Request) loyaltyrpc.Reply {
	userID, _, ok, reply := parseIDs(req, true)
	if !ok {
		return reply
	}
	err := l.repo.CounterDelete(ctx, userID, req.Name)
	return l.foundReply("loyalty counter.delete", true, err)
}

func (l *loyaltyRPC) handleCounterEntryDelete(ctx context.Context, req loyaltyrpc.Request) loyaltyrpc.Reply {
	userID, viewerID, ok, reply := parseIDs(req, true)
	if !ok {
		return reply
	}
	target := repository.SetTarget{ViewerID: viewerID, Command: req.Command}
	found, err := l.repo.CounterEntryDelete(ctx, userID, req.Name, target)
	return l.foundReply("loyalty counter.entry.delete", found, err)
}

func (l *loyaltyRPC) handleCounterBoard(ctx context.Context, req loyaltyrpc.Request) loyaltyrpc.Reply {
	rows, err := l.repo.CounterBoard(ctx, req.Name, req.Limit)
	if err != nil {
		return l.fail("loyalty counter.board", err)
	}
	board := make([]loyaltyrpc.CounterRank, 0, len(rows))
	for _, row := range rows {
		board = append(board, loyaltyrpc.CounterRank{
			UserID: strconv.FormatUint(row.UserID, 10),
			Value:  row.Value,
		})
	}
	return loyaltyrpc.Reply{Board: board, Found: true}
}

func (l *loyaltyRPC) handleCounterList(ctx context.Context, req loyaltyrpc.Request) loyaltyrpc.Reply {
	return l.counterRows(ctx, req, true, "loyalty counter.list", l.repo.CountersList)
}

func (l *loyaltyRPC) counterRows(ctx context.Context, req loyaltyrpc.Request, allowBotNS bool, label string, list func(context.Context, uint64) ([]*ent.Counter, error)) loyaltyrpc.Reply {
	userID, _, ok, reply := parseIDs(req, allowBotNS)
	if !ok {
		return reply
	}
	rows, err := list(ctx, userID)
	if err != nil {
		return l.fail(label, err)
	}
	counters := make([]loyaltyrpc.Counter, 0, len(rows))
	for _, row := range rows {
		counters = append(counters, loyaltyrpc.Counter{Name: row.Name, Scope: row.Scope, Value: row.Value})
	}
	return loyaltyrpc.Reply{Counters: counters, Found: true}
}
