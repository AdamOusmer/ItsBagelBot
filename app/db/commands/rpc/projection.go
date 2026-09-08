// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package rpc

import (
	"context"

	fetchkeyrpc "ItsBagelBot/internal/domain/rpc/fetchkey"
	"ItsBagelBot/internal/domain/rpc/projection"
	"ItsBagelBot/pkg/bus"
)

// SubscribeProjection serves the internal projection read: the worker's
// projection.Client falls through to it when a user's settings hash has no
// projected command section yet. The user-id guard lives in bus.ServeForUser,
// shared with every other user-scoped verb in the fleet.
func SubscribeProjection(w Wiring, subject string) error {
	return bus.ServeForUser[projection.Request, projection.CommandsReply](w.RPCWiring, subject,
		func(ctx context.Context, req projection.Request, id uint64) (projection.CommandsReply, error) {
			views, err := w.Commands.List(ctx, id)
			if err != nil {
				return projection.CommandsReply{}, err
			}
			return projection.CommandsReply{UserID: req.UserID, Commands: views}, nil
		})
}

// SubscribeFetchProjection serves the tier-3 fallback the worker's
// projection.Client falls through to when a user's settings hash has no
// projected fetch section yet — the exact role the commands get verb plays
// for command:<name> fields. The reply shape mirrors CommandsReply with
// fetch views (fetchkey.FetchView, whose tags match the Valkey field JSON).
func SubscribeFetchProjection(w Wiring, subject string) error {
	return bus.ServeForUser[fetchkeyrpc.FetchListRequest, fetchkeyrpc.FetchListReply](w.RPCWiring, subject,
		func(ctx context.Context, req fetchkeyrpc.FetchListRequest, id uint64) (fetchkeyrpc.FetchListReply, error) {
			views, err := w.Fetches.List(ctx, id)
			if err != nil {
				return fetchkeyrpc.FetchListReply{}, err
			}
			return fetchkeyrpc.FetchListReply{UserID: req.UserID, Fetches: views}, nil
		})
}
