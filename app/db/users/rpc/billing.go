// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package rpc

import (
	"context"

	billingrpc "ItsBagelBot/internal/domain/rpc/billing"
	"ItsBagelBot/pkg/bus"
)

func SubscribeBilling(w Wiring, subject string) error {
	repo := w.Repo
	return bus.Serve(w.Within(billingBudget), subject,
		func(ctx context.Context, req billingrpc.ApplyRequest) billingrpc.ApplyReply {
			applied, err := repo.ApplyBilling(ctx, req)
			if err != nil {
				return billingrpc.ApplyReply{Refusal: refusal(err)}
			}
			return billingrpc.ApplyReply{Applied: applied}
		},
	)
}
