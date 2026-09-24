// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package presence

import (
	"context"
	"errors"
	"time"

	"github.com/nats-io/nats.go"

	usersrpc "ItsBagelBot/internal/domain/rpc/users"
	"ItsBagelBot/pkg/bus"
)

const RefreshInterval = 5 * time.Minute

const rpcTimeout = 3 * time.Second

func NewFetch(nc *nats.Conn, subject string) Fetch {
	return func(ctx context.Context) (int, error) {
		ctx, cancel := context.WithTimeout(ctx, rpcTimeout)
		defer cancel()
		reply, err := bus.RequestJSON[usersrpc.CountsReply](ctx, nc, subject, usersrpc.CountsRequest{})
		if err != nil {
			return 0, err
		}
		if reply.Error != "" {
			return 0, errors.New(reply.Error)
		}
		return reply.TotalUsers, nil
	}
}
