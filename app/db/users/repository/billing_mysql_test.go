// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"ItsBagelBot/app/db/dbtest"
	"ItsBagelBot/app/db/users/ent"
	"ItsBagelBot/app/db/users/repository"
	billingrpc "ItsBagelBot/internal/domain/rpc/billing"
	"ItsBagelBot/internal/testdb"
	"ItsBagelBot/pkg/bus/bustest"

	entsql "entgo.io/ent/dialect/sql"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

const billingDeliveries = 12

func TestApplyBillingMySQLConcurrentDuplicateCountsGiftOnce(t *testing.T) {
	raw := testdb.MySQL(t, "bagel_billing_")
	client := ent.NewClient(ent.Driver(entsql.OpenDB("mysql", raw)))
	require.NoError(t, client.Schema.Create(t.Context()))
	repo := repository.NewUsers(client, dbtest.NewPacker(t), bustest.NewPublisher(), nil, zap.NewNop())
	t.Cleanup(func() { repo.Close(context.Background()) })
	ctx := t.Context()
	require.NoError(t, repo.Register(ctx, 4101, "Gifter", "Gifter", "gifter@example.com"))
	require.NoError(t, repo.Register(ctx, 4102, "Recipient", "Recipient", "recipient@example.com"))

	when := time.Now().UTC().Truncate(time.Second)
	expires := when.AddDate(0, 1, 0)
	gift := billingrpc.ApplyRequest{
		UserID: 4102, EventID: "evt-gift-race", Action: billingrpc.ActionActivate,
		OccurredAt: when, ExpiresAt: &expires, GifterID: 4101,
	}

	var wg sync.WaitGroup
	applied := make([]bool, billingDeliveries)
	errs := make([]error, billingDeliveries)
	for i := range billingDeliveries {
		wg.Go(func() { applied[i], errs[i] = repo.ApplyBilling(ctx, gift) })
	}
	wg.Wait()

	for i := range billingDeliveries {
		require.NoError(t, errs[i])
		assert.True(t, applied[i], "duplicate delivery must report the same success")
	}
	assert.Equal(t, uint32(1), client.User.GetX(ctx, 4101).GiftsSent, "duplicate deliveries must count the gift once")
}
