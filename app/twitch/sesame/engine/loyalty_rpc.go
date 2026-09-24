// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"context"
	"fmt"
	"strconv"
	"time"

	loyaltyrpc "ItsBagelBot/internal/domain/rpc/loyalty"
	"ItsBagelBot/pkg/bus"
	"ItsBagelBot/pkg/codec"

	"github.com/nats-io/nats.go"
)

const loyaltyRPCTimeout = 3 * time.Second

type LoyaltyRPC struct {
	nc     *nats.Conn
	prefix string
}

func NewLoyaltyRPC(nc *nats.Conn, prefix string) *LoyaltyRPC {
	return &LoyaltyRPC{nc: nc, prefix: prefix}
}

func (l *LoyaltyRPC) call(ctx context.Context, verb string, req loyaltyrpc.Request) (loyaltyrpc.Reply, error) {
	subject := l.prefix + "." + verb

	ctx, cancel := context.WithTimeout(ctx, loyaltyRPCTimeout)
	defer cancel()

	body, err := codec.Marshal(req)
	if err != nil {
		return loyaltyrpc.Reply{}, fmt.Errorf("rpc %s marshal request: %w", subject, err)
	}
	msg, err := bus.RequestWithContext(ctx, l.nc, subject, body)
	if err != nil {
		return loyaltyrpc.Reply{}, fmt.Errorf("rpc %s request: %w", subject, err)
	}
	var reply loyaltyrpc.Reply
	if err := codec.Unmarshal(msg.Data, &reply); err != nil {
		return loyaltyrpc.Reply{}, fmt.Errorf("rpc %s unmarshal reply: %w", subject, err)
	}
	if reply.Error != "" {
		return loyaltyrpc.Reply{}, fmt.Errorf("rpc %s: %s", subject, reply.Error)
	}
	return reply, nil
}

func fmtID(id uint64) string { return strconv.FormatUint(id, 10) }

func (l *LoyaltyRPC) BalanceGet(ctx context.Context, broadcasterID, viewerID uint64) (loyaltyrpc.Balance, error) {
	reply, err := l.call(ctx, "balance.get", loyaltyrpc.Request{UserID: fmtID(broadcasterID), ViewerID: fmtID(viewerID)})
	if err != nil {
		return loyaltyrpc.Balance{}, err
	}
	if reply.Balance == nil {
		return loyaltyrpc.Balance{}, nil
	}
	return *reply.Balance, nil
}

func (l *LoyaltyRPC) BalanceAdjust(ctx context.Context, broadcasterID uint64, viewerLogin string, value int64, absolute bool) (loyaltyrpc.Balance, bool, error) {
	verb := "balance.add"
	if absolute {
		verb = "balance.set"
	}
	reply, err := l.call(ctx, verb, loyaltyrpc.Request{UserID: fmtID(broadcasterID), ViewerLogin: viewerLogin, Value: value})
	if err != nil {
		return loyaltyrpc.Balance{}, false, err
	}
	if !reply.Found || reply.Balance == nil {
		return loyaltyrpc.Balance{}, false, nil
	}
	return *reply.Balance, true, nil
}

func (l *LoyaltyRPC) BalanceSpend(ctx context.Context, broadcasterID uint64, viewerLogin string, amount int64) (bal loyaltyrpc.Balance, found, spent bool, err error) {
	reply, err := l.call(ctx, "balance.spend", loyaltyrpc.Request{UserID: fmtID(broadcasterID), ViewerLogin: viewerLogin, Value: amount})
	if err != nil {
		return loyaltyrpc.Balance{}, false, false, err
	}
	if !reply.Found || reply.Balance == nil {
		return loyaltyrpc.Balance{}, false, false, nil
	}
	return *reply.Balance, true, reply.Spent, nil
}

func (l *LoyaltyRPC) BalanceTransfer(ctx context.Context, broadcasterID, fromViewerID uint64, targetLogin string, amount int64) (bal loyaltyrpc.Balance, target *loyaltyrpc.Balance, found, moved bool, err error) {
	reply, err := l.call(ctx, "balance.transfer", loyaltyrpc.Request{
		UserID:      fmtID(broadcasterID),
		ViewerID:    fmtID(fromViewerID),
		ViewerLogin: targetLogin,
		Value:       amount,
	})
	if err != nil {
		return loyaltyrpc.Balance{}, nil, false, false, err
	}
	if !reply.Found || reply.Balance == nil {
		return loyaltyrpc.Balance{}, nil, false, false, nil
	}
	return *reply.Balance, reply.TargetBalance, true, reply.Spent && reply.TargetBalance != nil, nil
}

func (l *LoyaltyRPC) Top(ctx context.Context, broadcasterID uint64, limit int) ([]loyaltyrpc.Balance, error) {
	reply, err := l.call(ctx, "top.get", loyaltyrpc.Request{UserID: fmtID(broadcasterID), Limit: limit})
	if err != nil {
		return nil, err
	}
	return reply.Top, nil
}

func (l *LoyaltyRPC) CounterGet(ctx context.Context, broadcasterID uint64, name string, viewerID uint64, command string) (loyaltyrpc.Counter, bool, error) {
	req := loyaltyrpc.Request{UserID: fmtID(broadcasterID), Name: name, Command: command}
	if viewerID != 0 {
		req.ViewerID = fmtID(viewerID)
	}
	reply, err := l.call(ctx, "counter.get", req)
	if err != nil {
		return loyaltyrpc.Counter{}, false, err
	}
	if !reply.Found || reply.Counter == nil {
		return loyaltyrpc.Counter{}, false, nil
	}
	return *reply.Counter, true, nil
}

func (l *LoyaltyRPC) CounterCreate(ctx context.Context, broadcasterID uint64, name, scope string) (loyaltyrpc.Counter, error) {
	reply, err := l.call(ctx, "counter.create", loyaltyrpc.Request{UserID: fmtID(broadcasterID), Name: name, Scope: scope})
	if err != nil {
		return loyaltyrpc.Counter{}, err
	}
	if reply.Counter == nil {
		return loyaltyrpc.Counter{}, fmt.Errorf("rpc %s.counter.create: empty reply", l.prefix)
	}
	return *reply.Counter, nil
}

func (l *LoyaltyRPC) CounterSet(ctx context.Context, broadcasterID uint64, name string, viewerID uint64, command string, value int64) (bool, error) {
	req := loyaltyrpc.Request{UserID: fmtID(broadcasterID), Name: name, Command: command, Value: value}
	if viewerID != 0 {
		req.ViewerID = fmtID(viewerID)
	}
	reply, err := l.call(ctx, "counter.set", req)
	if err != nil {
		return false, err
	}
	return reply.Found, nil
}

func (l *LoyaltyRPC) CounterDelete(ctx context.Context, broadcasterID uint64, name string) error {
	_, err := l.call(ctx, "counter.delete", loyaltyrpc.Request{UserID: fmtID(broadcasterID), Name: name})
	return err
}

func (l *LoyaltyRPC) CounterList(ctx context.Context, broadcasterID uint64) ([]loyaltyrpc.Counter, error) {
	reply, err := l.call(ctx, "counter.list", loyaltyrpc.Request{UserID: fmtID(broadcasterID)})
	if err != nil {
		return nil, err
	}
	return reply.Counters, nil
}
