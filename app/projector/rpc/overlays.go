// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package rpc

import (
	"context"
	"errors"
	"time"

	"github.com/nats-io/nats.go"

	"ItsBagelBot/internal/domain/invalidate"
	domainrpc "ItsBagelBot/internal/domain/rpc"
	overlaysrpc "ItsBagelBot/internal/domain/rpc/overlays"
	"ItsBagelBot/internal/projection"
	"ItsBagelBot/pkg/bus"
	"ItsBagelBot/pkg/cache"
)

const (
	poweredCacheCapacity int64 = 4096
	poweredCacheTTL            = 30 * time.Second
	poweredOwnerTimeout        = time.Second
	poweredReplyTimeout        = 1500 * time.Millisecond
)

type poweredEntry struct {
	overlay projection.PoweredOverlay
	found   bool
}

type poweredRPC struct {
	store    *projection.Store
	nc       *nats.Conn
	tokens   *cache.Cache[poweredEntry]
	overlays *cache.Cache[overlayEntry]
	premium  *cache.Cache[bool]
}

type PoweredConfig struct {
	Store         *projection.Store
	Invalidations cache.Feed
}

func SubscribePowered(w bus.RPCWiring, cfg PoweredConfig) error {
	s := &poweredRPC{
		store:    cfg.Store,
		nc:       w.NC,
		tokens:   cache.New[poweredEntry](poweredCacheCapacity, poweredCacheTTL).InvalidateOn(cfg.Invalidations, invalidatedTokens, invalidate.ScopeOverlays),
		overlays: cache.New[overlayEntry](poweredCacheCapacity, poweredCacheTTL).InvalidateOn(cfg.Invalidations, invalidatedTokens, invalidate.ScopeOverlays),
		premium:  cache.New[bool](poweredCacheCapacity, poweredCacheTTL).InvalidateOn(cfg.Invalidations, cache.UserKeys("premium:"), invalidate.ScopeUserProjection, invalidate.ScopeLive),
	}
	within := w.Within(poweredReplyTimeout)
	return errors.Join(
		bus.Serve(within, overlaysrpc.PoweredResolveSubject, s.handleResolve),
		bus.Serve(within, overlaysrpc.OverlayResolveSubject, s.handleOverlayResolve),
	)
}

func invalidatedTokens(e cache.Event) []string {
	return e.Keys
}

func (s *poweredRPC) handleResolve(ctx context.Context, req overlaysrpc.PoweredResolveRequest) overlaysrpc.PoweredResolveReply {
	if req.Token == "" {
		return overlaysrpc.PoweredResolveReply{Refusal: domainrpc.Refused(domainrpc.CodeInvalid, "token required")}
	}
	entry, err := s.tokens.GetOrLoad(ctx, req.Token, func(ctx context.Context) (poweredEntry, error) {
		return s.loadToken(ctx, req.Token)
	})
	if err != nil {
		return overlaysrpc.PoweredResolveReply{Refusal: domainrpc.Fail(err)}
	}
	if !entry.found {
		return overlaysrpc.PoweredResolveReply{Refusal: domainrpc.Refused(domainrpc.CodeNotFound, "overlay not found")}
	}
	premium, err := s.premiumOf(ctx, entry.overlay.UserID)
	if err != nil {
		return overlaysrpc.PoweredResolveReply{Refusal: domainrpc.Fail(err)}
	}
	return overlaysrpc.PoweredResolveReply{Config: entry.overlay.Config, Premium: premium}
}

func (s *poweredRPC) loadToken(ctx context.Context, token string) (poweredEntry, error) {
	overlay, found, err := s.store.PoweredOverlayByToken(ctx, token)
	if err == nil && found {
		return poweredEntry{overlay: overlay, found: true}, nil
	}
	return s.ownerToken(ctx, token)
}

func (s *poweredRPC) ownerToken(ctx context.Context, token string) (poweredEntry, error) {
	reply, err := bus.RequestJSONTimeout[overlaysrpc.OwnerPoweredReply](ctx, s.nc, overlaysrpc.OwnerPoweredByTokenSubject, overlaysrpc.OwnerPoweredByTokenRequest{Token: token}, poweredOwnerTimeout)
	if bus.NotFoundReply(err) {
		return poweredEntry{}, nil
	}
	if err != nil {
		return poweredEntry{}, err
	}
	if reply.Overlay == nil {
		return poweredEntry{}, nil
	}
	_, _ = s.store.SetPoweredOverlay(ctx, projection.PoweredWrite{Overlay: *reply.Overlay})
	return poweredEntry{overlay: *reply.Overlay, found: true}, nil
}

func (s *poweredRPC) premiumOf(ctx context.Context, userID uint64) (bool, error) {
	return s.premium.GetOrLoad(ctx, cache.UserKey("premium:", userID), func(ctx context.Context) (bool, error) {
		user, _, err := s.store.GetUserRuntime(ctx, userID)
		return user.Premium(), err
	})
}
