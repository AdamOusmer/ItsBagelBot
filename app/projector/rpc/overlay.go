// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package rpc

import (
	"context"

	"ItsBagelBot/internal/domain/event/data"
	"ItsBagelBot/internal/domain/overlays"
	domainrpc "ItsBagelBot/internal/domain/rpc"
	overlaysrpc "ItsBagelBot/internal/domain/rpc/overlays"
	"ItsBagelBot/pkg/bus"
)

type overlayEntry struct {
	overlay overlaysrpc.Overlay
	found   bool
}

func (s *poweredRPC) handleOverlayResolve(ctx context.Context, req overlaysrpc.OverlayResolveRequest) overlaysrpc.OverlayResolveReply {
	if !req.TokenHash.Valid() {
		return overlayNotFound()
	}
	entry, err := s.overlays.GetOrLoad(ctx, string(req.TokenHash), func(ctx context.Context) (overlayEntry, error) {
		return s.loadOverlay(ctx, req.TokenHash)
	})
	if err != nil {
		return overlaysrpc.OverlayResolveReply{Refusal: domainrpc.Fail(err)}
	}
	if !entry.found {
		return overlayNotFound()
	}
	premium, err := s.premiumOf(ctx, entry.overlay.UserID)
	if err != nil {
		return overlaysrpc.OverlayResolveReply{Refusal: domainrpc.Fail(err)}
	}
	alpha, err := s.alphaOf(ctx, entry.overlay.UserID)
	if err != nil {
		return overlaysrpc.OverlayResolveReply{Refusal: domainrpc.Fail(err)}
	}
	return resolveReply(entry.overlay, premium, alpha)
}

func resolveReply(o overlaysrpc.Overlay, premium, alpha bool) overlaysrpc.OverlayResolveReply {
	reply := overlaysrpc.OverlayResolveReply{
		UserID: o.UserID, OverlayID: o.ID, Kind: o.Kind, Template: o.Template,
		Revision: o.Revision, Generation: o.Generation, Premium: premium,
		Allowed: overlays.Allowed(overlays.Kind(o.Kind), alpha),
	}
	if reply.Allowed {
		reply.Document = o.Document
	}
	return reply
}

func overlayNotFound() overlaysrpc.OverlayResolveReply {
	return overlaysrpc.OverlayResolveReply{Refusal: domainrpc.Refused(domainrpc.CodeNotFound, "overlay not found")}
}

func (s *poweredRPC) loadOverlay(ctx context.Context, hash overlaysrpc.TokenHash) (overlayEntry, error) {
	overlay, found, err := s.store.OverlayByHash(ctx, hash)
	if err != nil || found {
		return overlayEntry{overlay: overlay, found: found}, err
	}
	reply, err := bus.RequestJSONTimeout[overlaysrpc.OwnerPoweredReply](ctx, s.nc, overlaysrpc.OwnerPoweredByTokenSubject, overlaysrpc.OwnerPoweredByTokenRequest{TokenHash: hash}, poweredOwnerTimeout)
	if bus.NotFoundReply(err) {
		return overlayEntry{}, nil
	}
	if err != nil {
		return overlayEntry{}, err
	}
	if reply.Unified == nil {
		return overlayEntry{}, nil
	}
	if _, err := s.store.FoldOverlay(ctx, data.OverlayChangedDTO{Overlay: *reply.Unified}); err != nil {
		return overlayEntry{}, err
	}
	return overlayEntry{overlay: *reply.Unified, found: true}, nil
}
