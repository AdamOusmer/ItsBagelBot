// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package rpc

import (
	"context"

	"ItsBagelBot/internal/domain/rpc/projection"
	"ItsBagelBot/pkg/bus"
)

func SubscribeProjection(w Wiring, subject string) error {
	return bus.ServeForUser[projection.Request, projection.UserReply](w.RPCWiring, subject,
		func(ctx context.Context, req projection.Request, id uint64) (projection.UserReply, error) {
			view, err := w.Repo.Get(ctx, id)
			if err != nil {
				return projection.UserReply{}, err
			}
			return projection.UserReply{
				UserID:             req.UserID,
				Status:             view.Status,
				IsActive:           view.IsActive,
				Banned:             view.Banned,
				Locale:             view.Locale,
				CommandsPageHidden: view.CommandsPageHidden,
			}, nil
		})
}
