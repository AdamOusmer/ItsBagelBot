// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package analytics

import (
	"context"
	"maps"
	"time"

	"ItsBagelBot/internal/domain/rpc"
	"ItsBagelBot/pkg/bus"
	"github.com/nats-io/nats.go"
	"golang.org/x/sync/errgroup"
)

func RequestLevels(nc *nats.Conn) Seed {
	return func(ctx context.Context) (Tally, error) {
		ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
		defer cancel()
		services := []rpc.LevelService{rpc.UsersLevels, rpc.ModulesLevels, rpc.CommandsLevels}
		replies := make([]rpc.LevelsReply, len(services))
		group, ctx := errgroup.WithContext(ctx)
		for i, service := range services {
			group.Go(func() error {
				return bus.RequestInto(ctx, nc, bus.RPCRequest{Subject: service.LevelsSubject(), Body: rpc.LevelsRequest{}}, &replies[i])
			})
		}
		if err := group.Wait(); err != nil {
			return nil, err
		}
		totals := Tally{}
		for _, reply := range replies {
			maps.Copy(totals, reply.Levels)
		}
		return totals, nil
	}
}
