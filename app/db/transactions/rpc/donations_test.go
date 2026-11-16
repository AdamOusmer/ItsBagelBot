// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package rpc

import (
	"context"
	"errors"
	"testing"
	"time"

	"ItsBagelBot/app/db/transactions/kofi"
	domainrpc "ItsBagelBot/internal/domain/rpc"
	transactionsrpc "ItsBagelBot/internal/domain/rpc/transactions"
	usersrpc "ItsBagelBot/internal/domain/rpc/users"
	"ItsBagelBot/internal/testnats"
	"ItsBagelBot/pkg/bus"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

type fixedLedger struct {
	rows     []kofi.Donation
	err      error
	from, to time.Time
}

func (l *fixedLedger) DonationsBetween(_ context.Context, from, to time.Time) ([]kofi.Donation, error) {
	l.from, l.to = from, to
	return l.rows, l.err
}

func TestDonationsRPC(t *testing.T) {
	now := time.Date(2026, 11, 16, 9, 0, 0, 0, time.UTC)
	paid := kofi.Donation{Kind: kofi.KindDonation, AmountCents: 500, Currency: "USD", OccurredAt: now.AddDate(0, 0, -2)}
	for _, tc := range []struct {
		name   string
		req    transactionsrpc.DonationsRequest
		err    error
		code   domainrpc.Code
		totals []transactionsrpc.DonationTotal
	}{
		{name: "summarizes the ledger for an admin", req: transactionsrpc.DonationsRequest{ActorID: "10", Days: 30},
			totals: []transactionsrpc.DonationTotal{{Currency: "USD", GrossCents: 500, Payments: 1}}},
		{name: "refuses a non-admin actor", req: transactionsrpc.DonationsRequest{ActorID: "11", Days: 30}, code: domainrpc.CodeForbidden},
		{name: "refuses an empty window", req: transactionsrpc.DonationsRequest{ActorID: "10"}, code: domainrpc.CodeInvalid},
		{name: "refuses an oversized window", req: transactionsrpc.DonationsRequest{ActorID: "10", Days: 401}, code: domainrpc.CodeInvalid},
		{name: "reports an unavailable ledger", req: transactionsrpc.DonationsRequest{ActorID: "10", Days: 30}, err: errors.New("db down"), code: domainrpc.CodeUnavailable},
	} {
		t.Run(tc.name, func(t *testing.T) {
			nc := testnats.Connect(t)
			w := bus.RPCWiring{NC: nc, Log: zap.NewNop()}
			require.NoError(t, bus.Serve(w, defaultUsersAuthSubject, func(_ context.Context, req usersrpc.AuthRequest) usersrpc.AuthReply {
				return usersrpc.AuthReply{Admin: req.UserID == "10", Role: "admin"}
			}))
			ledger := &fixedLedger{rows: []kofi.Donation{paid}, err: tc.err}
			require.NoError(t, SubscribeDonations(w, DonationsConfig{Users: NewUsersGiveawayClient(nc), Ledger: ledger, Now: func() time.Time { return now }}))

			reply := giveawayRequest[transactionsrpc.DonationsReply](t, nc, transactionsrpc.DonationsSubject, tc.req)
			assert.Equal(t, tc.code, reply.Code)
			assert.Equal(t, tc.totals, reply.Totals)
		})
	}
}

func TestDonationsRPCReadsPreviousWindowAndTrailingYear(t *testing.T) {
	nc := testnats.Connect(t)
	w := bus.RPCWiring{NC: nc, Log: zap.NewNop()}
	require.NoError(t, bus.Serve(w, defaultUsersAuthSubject, func(context.Context, usersrpc.AuthRequest) usersrpc.AuthReply {
		return usersrpc.AuthReply{Admin: true, Role: "admin"}
	}))
	ledger := &fixedLedger{}
	now := time.Date(2026, 11, 16, 9, 0, 0, 0, time.UTC)
	require.NoError(t, SubscribeDonations(w, DonationsConfig{Users: NewUsersGiveawayClient(nc), Ledger: ledger, Now: func() time.Time { return now }}))

	giveawayRequest[transactionsrpc.DonationsReply](t, nc, transactionsrpc.DonationsSubject, transactionsrpc.DonationsRequest{ActorID: "10", Days: 365})
	assert.Equal(t, [2]time.Time{time.Date(2024, 11, 16, 0, 0, 0, 0, time.UTC), time.Date(2026, 11, 16, 0, 0, 0, 0, time.UTC)}, [2]time.Time{ledger.from, ledger.to})
}
