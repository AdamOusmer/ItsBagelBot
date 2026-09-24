// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package rpc

import (
	"context"

	usersrpc "ItsBagelBot/internal/domain/rpc/users"
	"ItsBagelBot/pkg/bus"
)

// Internet-facing: must not grow the tier breakdown the admin stats verb reports.
func SubscribeCounts(w Wiring, subject string) error {
	repo := w.Repo
	return bus.Serve(w.Within(countsBudget), subject,
		func(ctx context.Context, _ usersrpc.CountsRequest) usersrpc.CountsReply {
			total, active, _, _, err := repo.UserStats(ctx)
			if err != nil {
				return usersrpc.CountsReply{Refusal: refusal(err)}
			}
			return usersrpc.CountsReply{TotalUsers: total, ActiveUsers: active}
		},
	)
}
