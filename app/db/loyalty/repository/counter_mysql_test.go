// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository

import (
	"context"
	"database/sql"
	"fmt"
	"sync"
	"testing"

	"ItsBagelBot/app/db/loyalty/ent"
	"ItsBagelBot/internal/domain/event/data"
	"ItsBagelBot/internal/testdb"

	entsql "entgo.io/ent/dialect/sql"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

const (
	replicaWriters  = 8
	writerBatches   = 40
	replicaChannels = 6
)

type innodbLocks struct{ deadlocks, waits int64 }

func readInnodbLocks(t *testing.T, raw *sql.DB) innodbLocks {
	t.Helper()
	var locks innodbLocks
	err := raw.QueryRow("SELECT SUM(IF(NAME='lock_deadlocks',`COUNT`,0)), SUM(IF(NAME='lock_row_lock_waits',`COUNT`,0)) FROM information_schema.INNODB_METRICS WHERE NAME IN ('lock_deadlocks','lock_row_lock_waits')").Scan(&locks.deadlocks, &locks.waits)
	require.NoError(t, err)
	return locks
}

func mysqlReplica(t *testing.T, raw *sql.DB) *Loyalty {
	t.Helper()
	drv := entsql.OpenDB("mysql", raw)
	client := ent.NewClient(ent.Driver(drv))
	require.NoError(t, MigrateSchema(t.Context(), client, drv.Dialect()))
	repo := NewLoyalty(client, drv, nil, zap.NewNop())
	require.NoError(t, repo.EnsureWatchSchema(t.Context()))
	t.Cleanup(func() { repo.Close(context.Background()) })
	return repo
}

func counterMySQLReplica(t *testing.T, raw *sql.DB) *CounterProcessor {
	t.Helper()
	return counterTestProcessor(t, mysqlReplica(t, raw), newCounterFakeValkey(t))
}

func replicaChannel(replica, writer, batch int) uint64 {
	return uint64(1 + 2*((writer+batch)%replicaChannels) + replica)
}

func replicaBatch(replica, writer, batch int) data.CounterBumpedDTO {
	return data.CounterBumpedDTO{
		BatchID: fmt.Sprintf("r%d-w%d-b%d", replica, writer, batch),
		UserID:  replicaChannel(replica, writer, batch),
		Bumps: []data.CounterBumpEntry{
			{Name: "deaths", Delta: 1},
			{Name: "hugs", Scope: data.CounterScopeViewer, ViewerID: uint64(1 + writer*writerBatches + batch), Delta: 1},
		},
	}
}

func flushReplica(replica int, p *CounterProcessor, errs chan<- error) {
	var writers sync.WaitGroup
	for writer := range replicaWriters {
		writers.Go(func() {
			for batch := range writerBatches {
				errs <- p.Process(context.Background(), replicaBatch(replica, writer, batch))
			}
		})
	}
	writers.Wait()
}

func TestCounterMySQLReplicasFlushOtherChannelsWithoutLockConflicts(t *testing.T) {
	raw := testdb.MySQL(t, "bagel_counter_")
	replicas := []*CounterProcessor{counterMySQLReplica(t, raw), counterMySQLReplica(t, raw)}
	before := readInnodbLocks(t, raw)
	errs := make(chan error, len(replicas)*replicaWriters*writerBatches)
	var flushes sync.WaitGroup
	for replica, p := range replicas {
		flushes.Go(func() { flushReplica(replica, p, errs) })
	}
	flushes.Wait()
	close(errs)
	for err := range errs {
		require.NoError(t, err)
	}
	after := readInnodbLocks(t, raw)

	assert.Zero(t, after.deadlocks-before.deadlocks, "replicas flushing different channels deadlocked")
	assert.Zero(t, after.waits-before.waits, "replicas flushing different channels waited on each other's row locks")
	var total, entries int64
	require.NoError(t, raw.QueryRow("SELECT SUM(value) FROM counters WHERE name='deaths'").Scan(&total))
	require.NoError(t, raw.QueryRow("SELECT SUM(value) FROM counter_entries WHERE name='hugs'").Scan(&entries))
	assert.EqualValues(t, len(replicas)*replicaWriters*writerBatches, total)
	assert.EqualValues(t, len(replicas)*replicaWriters*writerBatches, entries)
}
