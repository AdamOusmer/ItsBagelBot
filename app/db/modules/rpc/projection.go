// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package rpc

import (
	"context"

	"ItsBagelBot/internal/domain/rpc/projection"
	"ItsBagelBot/pkg/bus"
)

// SubscribeProjection serves the internal projection read the worker falls
// through to when a user's settings hash has no projected module section yet.
// The user-id guard lives in bus.ServeForUser, shared with every other
// user-scoped verb in the fleet.
func SubscribeProjection(w Wiring, subject string) error {
	return bus.ServeForUser[projection.Request, projection.ModulesReply](w.RPCWiring, subject,
		func(ctx context.Context, req projection.Request, id uint64) (projection.ModulesReply, error) {
			views, err := w.Repo.List(ctx, id)
			if err != nil {
				return projection.ModulesReply{}, err
			}
			return projection.ModulesReply{UserID: req.UserID, Modules: views}, nil
		})
}
