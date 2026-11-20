// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"context"
	"errors"
	"fmt"
	"time"

	"ItsBagelBot/internal/domain/rpc"
	loyaltyrpc "ItsBagelBot/internal/domain/rpc/loyalty"
	"ItsBagelBot/pkg/bus"
	"ItsBagelBot/pkg/cache"
)

type FreezeCommand struct {
	BroadcasterID uint64
	Holder        string
	FreezeID      string
	Minutes       int
	BlockTransfer bool
	CloseGames    bool
}

func LoyaltyFreezeRefusal(err error, reason string) bool {
	var reply bus.RPCReplyError
	return errors.As(err, &reply) && matchesFreezeConflict(reply, reason)
}

func matchesFreezeConflict(reply bus.RPCReplyError, reason string) bool {
	return reply.Code == rpc.CodeConflict && reply.Message == reason
}

func (l *LoyaltyRPC) FreezeGet(ctx context.Context, channel uint64) (loyaltyrpc.FreezeView, error) {
	reply, err := l.call(ctx, loyaltyrpc.VerbFreezeGet, loyaltyrpc.Request{UserID: fmtID(channel)})
	if err != nil || !hasFreeze(&reply) {
		return loyaltyrpc.FreezeView{}, err
	}
	return *reply.Freeze, nil
}

func hasFreeze(reply *loyaltyrpc.Reply) bool {
	return reply.Found && reply.Freeze != nil
}

func (l *LoyaltyRPC) FreezeSet(ctx context.Context, command FreezeCommand) (loyaltyrpc.FreezeView, bool, error) {
	return l.freeze(ctx, loyaltyrpc.VerbFreezeSet, command)
}

func (l *LoyaltyRPC) FreezeRelease(ctx context.Context, command FreezeCommand) (loyaltyrpc.FreezeView, bool, error) {
	return l.freeze(ctx, loyaltyrpc.VerbFreezeRelease, command)
}

func (l *LoyaltyRPC) freeze(ctx context.Context, verb loyaltyVerb, command FreezeCommand) (loyaltyrpc.FreezeView, bool, error) {
	reply, err := l.call(ctx, verb, loyaltyrpc.Request{
		UserID: fmtID(command.BroadcasterID), Holder: command.Holder,
		FreezeID: command.FreezeID, Minutes: command.Minutes, BlockTransfer: command.BlockTransfer, CloseGames: command.CloseGames,
	})
	if err != nil {
		return loyaltyrpc.FreezeView{}, false, err
	}
	if reply.Freeze == nil {
		return loyaltyrpc.FreezeView{}, false, fmt.Errorf("rpc %s.%s: empty freeze reply", l.prefix, verb)
	}
	return *reply.Freeze, reply.Found, nil
}

func (s *ValkeyLoyaltyStore) FreezeSet(ctx context.Context, command FreezeCommand) (loyaltyrpc.FreezeView, bool, error) {
	return s.freeze(ctx, loyaltyrpc.VerbFreezeSet, command)
}

func (s *ValkeyLoyaltyStore) FreezeGet(ctx context.Context, channel uint64) (loyaltyrpc.FreezeView, error) {
	return s.rpc.FreezeGet(ctx, channel)
}

func (s *ValkeyLoyaltyStore) FreezeRelease(ctx context.Context, command FreezeCommand) (loyaltyrpc.FreezeView, bool, error) {
	return s.freeze(ctx, loyaltyrpc.VerbFreezeRelease, command)
}

func (s *ValkeyLoyaltyStore) freeze(ctx context.Context, verb loyaltyVerb, command FreezeCommand) (loyaltyrpc.FreezeView, bool, error) {
	view, changed, err := s.rpc.freeze(ctx, verb, command)
	if err == nil && changed {
		s.invalidateTop(cache.Event{ID: command.BroadcasterID})
	}
	return view, changed, err
}

func freezeTTL(stamp loyaltyrpc.FreezeStamp, base time.Duration) time.Duration {
	if stamp.ExpiresAt == 0 {
		return base
	}
	return max(time.Nanosecond, min(base-time.Nanosecond, time.Until(time.UnixMilli(stamp.ExpiresAt))))
}
