// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository_test

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"ItsBagelBot/app/db/commands/ent"
	"ItsBagelBot/app/db/commands/ent/fetchdefinition"
	"ItsBagelBot/app/db/commands/repository"
	"ItsBagelBot/app/db/dbtest"
	"ItsBagelBot/internal/domain/validate"
	"ItsBagelBot/internal/testdb"
	"ItsBagelBot/pkg/bus/bustest"

	entsql "entgo.io/ent/dialect/sql"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

const fetchQuotaRacers = 8

func TestFetchDefQuotaHoldsUnderConcurrentCreatesMySQL(t *testing.T) {
	raw := testdb.MySQL(t, "bagel_fetchquota_")
	client := ent.NewClient(ent.Driver(entsql.OpenDB("mysql", raw)))
	require.NoError(t, client.Schema.Create(t.Context()))
	repo := repository.NewFetches(client, dbtest.NewPacker(t), bustest.NewPublisher(), zap.NewNop())
	t.Cleanup(repo.Close)

	const userID = 1001
	ctx := context.Background()
	for i := range validate.MaxFetchDefsPerBroadcaster - 1 {
		require.NoError(t, repo.UpsertDef(ctx, userID, fetchSpec(fmt.Sprint("seed", i), "https://seed.example.com")))
	}

	start := make(chan struct{})
	errs := make(chan error, fetchQuotaRacers)
	var racers sync.WaitGroup
	for i := range fetchQuotaRacers {
		racers.Add(1)
		go func() {
			defer racers.Done()
			<-start
			errs <- repo.UpsertDef(ctx, userID, fetchSpec(fmt.Sprint("racer", i), "https://racer.example.com"))
		}()
	}
	close(start)
	racers.Wait()
	close(errs)

	accepted := 0
	for err := range errs {
		if err == nil {
			accepted++
			continue
		}
		assert.Contains(t, err.Error(), "limit reached")
	}
	assert.Equal(t, 1, accepted)
	assert.Equal(t, validate.MaxFetchDefsPerBroadcaster, client.FetchDefinition.Query().Where(fetchdefinition.UserIDEQ(userID)).CountX(ctx))
}
