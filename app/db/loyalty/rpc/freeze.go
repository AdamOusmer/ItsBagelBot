// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package rpc

import (
	"context"
	"regexp"
	"slices"
	"time"

	"ItsBagelBot/app/db/loyalty/repository"
	"ItsBagelBot/internal/domain/invalidate"
	domainrpc "ItsBagelBot/internal/domain/rpc"
	loyaltyrpc "ItsBagelBot/internal/domain/rpc/loyalty"
)

var roundHolder = regexp.MustCompile(`^round:[a-z0-9-]{1,64}$`)

func validHolder(holder repository.FreezeHolder) bool {
	return holder == loyaltyrpc.FreezeModHolder || roundHolder.MatchString(string(holder))
}

func (l *loyaltyRPC) freezeHandler(release bool) func(context.Context, loyaltyrpc.Request) loyaltyrpc.Reply {
	return func(ctx context.Context, req loyaltyrpc.Request) loyaltyrpc.Reply {
		userID, _, ok, reply := parseIDs(req, false)
		if !ok {
			return reply
		}
		holder := repository.FreezeHolder(req.Holder)
		if !validHolder(holder) {
			return refuse(domainrpc.CodeInvalid, "invalid holder")
		}
		return l.applyFreeze(ctx, req, freezeTarget{userID, holder}, release)
	}
}

type freezeTarget struct {
	userID uint64
	holder repository.FreezeHolder
}

func (l *loyaltyRPC) applyFreeze(ctx context.Context, req loyaltyrpc.Request, target freezeTarget, release bool) loyaltyrpc.Reply {
	var state repository.FreezeState
	var changed bool
	var err error
	if release {
		state, changed, err = l.repo.ReleaseFreeze(ctx, repository.FreezeRelease{UserID: target.userID, Holder: target.holder, FreezeID: repository.FreezeID(req.FreezeID)})
	} else {
		flags := repository.FreezeFlags{BlockTransfer: req.BlockTransfer, CloseGames: req.CloseGames}
		state, changed, err = l.repo.Freeze(ctx, repository.FreezeRequest{UserID: target.userID, Holder: target.holder, Minutes: req.Minutes, Flags: flags})
	}
	return l.freezeResult(target.userID, state, changed, err)
}

func (l *loyaltyRPC) handleFreezeGet(ctx context.Context, req loyaltyrpc.Request) loyaltyrpc.Reply {
	userID, _, ok, reply := parseIDs(req, false)
	if !ok {
		return reply
	}
	state, err := l.repo.FreezeStatus(ctx, userID)
	if err != nil {
		return l.fail("loyalty freeze.get", err)
	}
	now := time.Now()
	return loyaltyrpc.Reply{Freeze: freezeView(state, now), Found: state.Active(now)}
}

func (l *loyaltyRPC) freezeResult(userID uint64, state repository.FreezeState, changed bool, err error) loyaltyrpc.Reply {
	if err != nil {
		return l.fail("loyalty freeze", err)
	}
	if changed {
		l.freezeChanged(userID)
	}
	return loyaltyrpc.Reply{Freeze: freezeView(state, time.Now()), Found: changed}
}

func freezeStamp(state repository.FreezeState) loyaltyrpc.FreezeStamp {
	return loyaltyrpc.FreezeStamp{FreezeID: string(state.FreezeID), FrozenAsOf: state.AsOf.UnixMilli(), ExpiresAt: state.ExpiresAt.UnixMilli()}
}

func freezeView(state repository.FreezeState, now time.Time) *loyaltyrpc.FreezeView {
	view := &loyaltyrpc.FreezeView{Holders: []string{}}
	if !state.Active(now) {
		return view
	}
	view.FreezeStamp = freezeStamp(state)
	view.BlockTransfer, view.CloseGames = state.Flags.BlockTransfer, state.Flags.CloseGames
	for holder, end := range state.Holders {
		if end > now.UnixMilli() {
			view.Holders = append(view.Holders, string(holder))
		}
	}
	slices.Sort(view.Holders)
	return view
}

func (l *loyaltyRPC) freezeChanged(userID uint64) {
	l.invalidations.Publish(invalidate.ScopeLoyalty, userID, invalidate.LoyaltyFreezeMarker)
}
