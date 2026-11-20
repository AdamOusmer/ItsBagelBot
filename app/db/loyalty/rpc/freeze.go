// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package rpc

import (
	"context"
	"time"

	"ItsBagelBot/app/db/loyalty/repository"
	"ItsBagelBot/internal/domain/invalidate"
	domainrpc "ItsBagelBot/internal/domain/rpc"
	loyaltyrpc "ItsBagelBot/internal/domain/rpc/loyalty"
	"ItsBagelBot/pkg/bus"
)

func (l *loyaltyRPC) subscribeFreeze(w bus.RPCWiring, prefix string) error {
	if err := bus.Serve(w, prefix+"."+loyaltyrpc.VerbFreezeSet, l.handleFreezeSet); err != nil {
		return err
	}
	if err := bus.Serve(w, prefix+"."+loyaltyrpc.VerbFreezeRelease, l.handleFreezeRelease); err != nil {
		return err
	}
	return bus.Serve(w, prefix+"."+loyaltyrpc.VerbFreezeGet, l.handleFreezeGet)
}

func (l *loyaltyRPC) handleFreezeSet(ctx context.Context, req loyaltyrpc.FreezeSetRequest) loyaltyrpc.FreezeTimes {
	userID, err := bus.NonZeroUserID(req.UserID)
	if err != nil {
		return loyaltyrpc.FreezeTimes{Refusal: invalidStandings(err)}
	}
	state, err := l.repo.Freeze(ctx, repository.FreezeRequest{UserID: userID, Minutes: req.Minutes})
	if err != nil {
		return loyaltyrpc.FreezeTimes{Refusal: l.fail("loyalty freeze.set", err).Refusal}
	}
	l.freezeChanged(userID)
	return freezeTimes(state)
}

func (l *loyaltyRPC) handleFreezeRelease(ctx context.Context, req loyaltyrpc.FreezeReleaseRequest) loyaltyrpc.FreezeReleased {
	userID, err := bus.NonZeroUserID(req.UserID)
	if err != nil {
		return loyaltyrpc.FreezeReleased{Refusal: invalidStandings(err)}
	}
	released, err := l.repo.ReleaseFreeze(ctx, userID)
	if err != nil {
		return loyaltyrpc.FreezeReleased{Refusal: l.fail("loyalty freeze.release", err).Refusal}
	}
	if released {
		l.freezeChanged(userID)
	}
	return loyaltyrpc.FreezeReleased{Released: released}
}

func (l *loyaltyRPC) handleFreezeGet(ctx context.Context, req loyaltyrpc.FreezeGetRequest) loyaltyrpc.FreezeView {
	userID, err := bus.NonZeroUserID(req.UserID)
	if err != nil {
		return loyaltyrpc.FreezeView{FreezeTimes: loyaltyrpc.FreezeTimes{Refusal: invalidStandings(err)}}
	}
	state, err := l.repo.FreezeStatus(ctx, userID)
	if err != nil {
		return loyaltyrpc.FreezeView{FreezeTimes: loyaltyrpc.FreezeTimes{Refusal: l.fail("loyalty freeze.get", err).Refusal}}
	}
	if !state.Active(time.Now()) {
		return loyaltyrpc.FreezeView{}
	}
	return loyaltyrpc.FreezeView{Active: true, FreezeTimes: freezeTimes(state)}
}

func invalidStandings(err error) domainrpc.Refusal {
	return domainrpc.Refused(domainrpc.CodeInvalid, err.Error())
}

func freezeTimes(state repository.FreezeState) loyaltyrpc.FreezeTimes {
	return loyaltyrpc.FreezeTimes{FrozenAsOf: state.AsOf.UnixMilli(), ExpiresAt: state.ExpiresAt.UnixMilli()}
}

func (l *loyaltyRPC) freezeChanged(userID uint64) {
	l.invalidations.Publish(invalidate.ScopeLoyalty, userID, invalidate.LoyaltyFreezeMarker)
}
