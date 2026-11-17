// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository_test

import (
	"context"
	"testing"
	"time"

	"ItsBagelBot/app/db/transactions/ent/enttest"
	"ItsBagelBot/app/db/transactions/kofi"
	"ItsBagelBot/app/db/transactions/repository"
	"ItsBagelBot/internal/testdb"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDonationsRecordOnceAndReadByTime(t *testing.T) {
	client := enttest.Open(t, testdb.Driver, testdb.MemDSN("kofi-donations"))
	t.Cleanup(func() { _ = client.Close() })
	repo := repository.NewDonations(client)
	ctx := context.Background()

	at := time.Date(2026, 11, 2, 13, 4, 30, 0, time.UTC)
	first := kofi.Donation{
		TransactionID: "t-1", MessageID: "m-1", Kind: kofi.KindSubscription, AmountCents: 550, Currency: "USD",
		Subscription: true, FirstSubscription: true, TierName: "Bagel tier", OccurredAt: at,
	}
	require.NoError(t, repo.RecordDonation(ctx, first))
	redelivered := first
	redelivered.MessageID, redelivered.AmountCents = "m-2", 9999
	require.NoError(t, repo.RecordDonation(ctx, redelivered))
	require.NoError(t, repo.RecordDonation(ctx, kofi.Donation{
		TransactionID: "t-2", MessageID: "m-3", Kind: kofi.KindDonation, AmountCents: 300, Currency: "EUR", OccurredAt: at.AddDate(0, 0, 5),
	}))

	rows, err := repo.DonationsBetween(ctx, at, at.AddDate(0, 0, 5))
	require.NoError(t, err)
	assert.Equal(t, []kofi.Donation{first}, rows)
}
