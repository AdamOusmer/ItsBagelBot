// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package rpc

import (
	"context"
	"slices"
	"time"

	"ItsBagelBot/app/db/overlays/assets"
	"ItsBagelBot/app/db/overlays/repository"
	domainrpc "ItsBagelBot/internal/domain/rpc"
	overlaysrpc "ItsBagelBot/internal/domain/rpc/overlays"
	"ItsBagelBot/pkg/bus"
)

const readBudget = 3 * time.Second

type Wiring struct {
	bus.RPCWiring
	Repo   *repository.Overlays
	Assets *assets.Service
}

func SubscribeDashboard(w Wiring) error {
	read := w.Within(readBudget)
	if err := bus.ServeForUser[overlaysrpc.PoweredGetRequest, overlaysrpc.PoweredView](read, overlaysrpc.PoweredGetSubject,
		func(ctx context.Context, _ overlaysrpc.PoweredGetRequest, id uint64) (overlaysrpc.PoweredView, error) {
			return viewOf(w.Repo.Get(ctx, id)), nil
		}); err != nil {
		return err
	}
	if err := bus.ServeForUser[overlaysrpc.PoweredSaveRequest, overlaysrpc.PoweredView](read, overlaysrpc.PoweredSaveSubject,
		func(ctx context.Context, req overlaysrpc.PoweredSaveRequest, id uint64) (overlaysrpc.PoweredView, error) {
			return viewOf(w.Repo.Save(ctx, id, req.Config, req.Revision)), nil
		}); err != nil {
		return err
	}
	return bus.ServeForUser[overlaysrpc.PoweredRotateRequest, overlaysrpc.PoweredView](read, overlaysrpc.PoweredRotateSubject,
		func(ctx context.Context, _ overlaysrpc.PoweredRotateRequest, id uint64) (overlaysrpc.PoweredView, error) {
			return viewOf(w.Repo.Rotate(ctx, id)), nil
		})
}

func SubscribeOwner(w Wiring) error {
	read := w.Within(readBudget)
	return bus.Serve(read, overlaysrpc.OwnerPoweredByTokenSubject, w.ownerLookup)
}

func (w Wiring) ownerLookup(ctx context.Context, req overlaysrpc.OwnerPoweredByTokenRequest) overlaysrpc.OwnerPoweredReply {
	if req.TokenHash != "" {
		return w.ownerUnified(ctx, req.TokenHash)
	}
	return ownerOf(w.Repo.ByToken(ctx, req.Token))
}

func viewOf(overlay overlaysrpc.PoweredOverlay, err error) overlaysrpc.PoweredView {
	if err != nil {
		return overlaysrpc.PoweredView{Refusal: refusal(err)}
	}
	return overlaysrpc.PoweredView{URL: overlaysrpc.PoweredURLPrefix + overlay.Token, Config: overlay.Config, Revision: overlay.Revision}
}

func ownerOf(overlay overlaysrpc.PoweredOverlay, err error) overlaysrpc.OwnerPoweredReply {
	if err != nil {
		return overlaysrpc.OwnerPoweredReply{Refusal: refusal(err)}
	}
	return overlaysrpc.OwnerPoweredReply{Overlay: &overlay}
}

var refusalRules = slices.Concat([]domainrpc.Rule{
	domainrpc.Is(repository.ErrNotFound, domainrpc.CodeNotFound),
	domainrpc.Is(repository.ErrRevisionConflict, domainrpc.CodeConflict),
	domainrpc.Is(repository.ErrInvalidConfig, domainrpc.CodeInvalid),
	domainrpc.Is(repository.ErrUnknownTemplate, domainrpc.CodeInvalid),
	domainrpc.Is(repository.ErrNotConfigurable, domainrpc.CodeInvalid),
	domainrpc.Is(repository.ErrTtsPlan, domainrpc.CodeForbidden),
}, assetRules)

func refusal(err error) domainrpc.Refusal {
	return bus.Classify(err, refusalRules...)
}
