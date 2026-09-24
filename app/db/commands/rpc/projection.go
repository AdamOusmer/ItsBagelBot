// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package rpc

import (
	"context"

	fetchkeyrpc "ItsBagelBot/internal/domain/rpc/fetchkey"
	"ItsBagelBot/internal/domain/rpc/projection"
	"ItsBagelBot/pkg/bus"
)

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
