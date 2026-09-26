// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package rpc

import (
	"context"

	"ItsBagelBot/app/db/users/ent"
	domainrpc "ItsBagelBot/internal/domain/rpc"

	"ItsBagelBot/internal/domain/rpc/projection"
	"ItsBagelBot/pkg/bus"
)

func SubscribeProjection(w Wiring, subject string) error {
	return bus.ServeForUser[projection.Request, projection.UserReply](w.RPCWiring, subject,
		func(ctx context.Context, req projection.Request, id uint64) (projection.UserReply, error) {
			view, err := w.Repo.Get(ctx, id)
			if ent.IsNotFound(err) {
				return projection.UserReply{UserID: req.UserID, Refusal: domainrpc.Refused(domainrpc.CodeNotFound, "user account not found")}, nil
			}
			if err != nil {
				return projection.UserReply{}, err
			}
			return projection.UserReply{
				AccountCreatedAt:   view.AccountCreatedAt,
				StateRevision:      view.StateRevision,
				UserID:             req.UserID,
				Status:             view.Status,
				IsActive:           view.IsActive,
				Banned:             view.Banned,
				Locale:             view.Locale,
				CommandsPageHidden: view.CommandsPageHidden,
			}, nil
		})
}
