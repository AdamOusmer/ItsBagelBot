// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package rpc

import (
	"context"
	"time"

	"ItsBagelBot/app/db/transactions/kofi"
	"ItsBagelBot/internal/domain/rpc"
	transactionsrpc "ItsBagelBot/internal/domain/rpc/transactions"
	"ItsBagelBot/pkg/bus"
)

type DonationLedger interface {
	DonationsBetween(ctx context.Context, from, to time.Time) ([]kofi.Donation, error)
}

type ActorAuthorizer interface {
	Authorize(context.Context, string) (uint64, error)
}

type DonationsConfig struct {
	Users  ActorAuthorizer
	Ledger DonationLedger
	Now    func() time.Time
}

type donationsRPC struct{ cfg DonationsConfig }

func SubscribeDonations(w bus.RPCWiring, cfg DonationsConfig) error {
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	d := &donationsRPC{cfg: cfg}
	return bus.Serve(w.Within(adminConsoleBudget), transactionsrpc.DonationsSubject, d.summary)
}

func (d *donationsRPC) summary(ctx context.Context, req transactionsrpc.DonationsRequest) transactionsrpc.DonationsReply {
	if _, err := d.cfg.Users.Authorize(ctx, req.ActorID); err != nil {
		return transactionsrpc.DonationsReply{Refusal: forbidden(err)}
	}
	if req.Days < 1 || req.Days > transactionsrpc.MaxDonationDays {
		return transactionsrpc.DonationsReply{Refusal: rpc.Refused(rpc.CodeInvalid, "days must be between 1 and 400")}
	}
	window := kofi.WindowFor(d.cfg.Now(), req.Days)
	rows, err := d.cfg.Ledger.DonationsBetween(ctx, window.From(), window.End)
	if err != nil {
		return transactionsrpc.DonationsReply{Refusal: rpc.Refused(rpc.CodeUnavailable, "donation ledger unavailable")}
	}
	return kofi.Summarize(rows, window)
}
