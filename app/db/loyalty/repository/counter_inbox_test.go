// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"ItsBagelBot/internal/domain/event/data"
	"ItsBagelBot/internal/testdb"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
)

func TestCounterDurableInbox(t *testing.T) {
	for _, dialect := range []string{"sqlite", "mysql"} {
		t.Run(dialect, func(t *testing.T) {
			for _, scenario := range []struct {
				name string
				run  func(*testing.T, *Loyalty, *sql.DB)
			}{
				{"commitWithoutCacheReceipt", counterCrashGap},
				{"concurrentReplicas", counterConcurrentReplay},
				{"digestMismatchIsolated", counterDigestMismatch},
				{"uncachedMismatchIsolated", counterUncachedMismatch},
				{"poisonSQLIsolated", counterPoisonSQL},
				{"binaryBatchIdentity", counterBinaryIdentity},
				{"largePayloadChunks", counterLargePayload},
				{"receiptRetention", counterReceiptPruning},
			} {
				t.Run(scenario.name, func(t *testing.T) {
					repo, raw := durableCounterRepo(t, dialect)
					scenario.run(t, repo, raw)
				})
			}
		})
	}
}

func durableCounterRepo(t *testing.T, dialect string) (*Loyalty, *sql.DB) {
	t.Helper()
	if dialect == "mysql" {
		raw := testdb.MySQL(t, "loyalty_receipts_")
		return mysqlReplica(t, raw), raw
	}
	return counterTestRepo(t)
}

func uncachedCounterProcessor(t *testing.T, repo *Loyalty) *CounterProcessor {
	t.Helper()
	cache := newCounterFakeValkey(t)
	cache.outage(true, true)
	return counterTestProcessor(t, repo, cache)
}

func counterCrashGap(t *testing.T, repo *Loyalty, raw *sql.DB) {
	p := uncachedCounterProcessor(t, repo)
	event := counterTestEvent("committed-without-marker", 7)
	require.NoError(t, p.Process(t.Context(), event))
	require.NoError(t, p.Close(context.Background()))
	p = counterTestProcessor(t, repo, newCounterFakeValkey(t))
	require.NoError(t, p.Process(t.Context(), event))
	require.EqualValues(t, 7, counterTestValue(t, raw, "deaths"))
	assertCounterReceipts(t, raw, 1)
}

func counterConcurrentReplay(t *testing.T, repo *Loyalty, raw *sql.DB) {
	other := repo
	if repo.dialect == "mysql" {
		other = mysqlReplica(t, raw)
	}
	processors := []*CounterProcessor{uncachedCounterProcessor(t, repo), uncachedCounterProcessor(t, other)}
	const copies = 24
	events := make([]data.CounterBumpedDTO, copies)
	for i := range events {
		events[i] = counterTestEvent(fmt.Sprintf("replicated-%d", i%3), 2)
	}
	errs := runCounterCopies(t.Context(), processors, events)
	for _, err := range errs {
		require.NoError(t, err)
	}
	require.EqualValues(t, 6, counterTestValue(t, raw, "deaths"))
	assertCounterReceipts(t, raw, 3)
}

func runCounterCopies(ctx context.Context, processors []*CounterProcessor, events []data.CounterBumpedDTO) []error {
	errs := make([]error, len(events))
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i, event := range events {
		wg.Go(func() {
			<-start
			errs[i] = processors[i%len(processors)].Process(ctx, event)
		})
	}
	close(start)
	wg.Wait()
	return errs
}

func counterDigestMismatch(t *testing.T, repo *Loyalty, raw *sql.DB) {
	logs, observed := observer.New(zap.WarnLevel)
	repo.log = zap.New(logs)
	p := counterTestProcessor(t, repo, newCounterFakeValkey(t))
	require.NoError(t, p.Process(t.Context(), counterTestEvent("stable", 2)))
	events := []data.CounterBumpedDTO{counterTestEvent("stable", 9), counterTestEvent("healthy", 3), counterTestEvent("stable", 2)}
	errs := runCounterCopies(t.Context(), []*CounterProcessor{p}, events)
	for i, err := range errs {
		if i == 0 {
			require.ErrorIs(t, err, ErrCounterReceiptMismatch)
			continue
		}
		require.NoError(t, err)
	}
	require.EqualValues(t, 5, counterTestValue(t, raw, "deaths"))
	assertCounterReceipts(t, raw, 2)
	require.ErrorIs(t, ErrCounterReceiptMismatch, ErrInvalidInput)
	entries := observed.FilterMessage("loyalty: counter receipt payload mismatch").All()
	require.Len(t, entries, 1)
	fields := entries[0].ContextMap()
	require.Equal(t, []any{uint64(1), "stable"}, []any{fields["user_id"], fields["batch_id"]})
}

func counterUncachedMismatch(t *testing.T, repo *Loyalty, raw *sql.DB) {
	p := uncachedCounterProcessor(t, repo)
	require.NoError(t, p.Process(t.Context(), counterTestEvent("stable", 2)))
	events := []data.CounterBumpedDTO{counterTestEvent("stable", 9), counterTestEvent("healthy", 3), counterTestEvent("stable", 2)}
	errs := runCounterCopies(t.Context(), []*CounterProcessor{p}, events)
	require.ErrorIs(t, errs[0], ErrCounterReceiptMismatch)
	for _, err := range errs[1:] {
		require.NoError(t, err)
	}
	require.EqualValues(t, 5, counterTestValue(t, raw, "deaths"))
	assertCounterReceipts(t, raw, 2)
}

func counterPoisonSQL(t *testing.T, repo *Loyalty, raw *sql.DB) {
	trigger := "CREATE TRIGGER counter_poison BEFORE INSERT ON counters FOR EACH ROW BEGIN IF NEW.name='poison' THEN SIGNAL SQLSTATE '45000' SET MYSQL_ERRNO=3819, MESSAGE_TEXT='poison counter'; END IF; END"
	if repo.dialect == "sqlite3" {
		_, err := raw.Exec("CREATE TABLE counter_poison_guard (name TEXT CHECK(name <> 'poison'))")
		require.NoError(t, err)
		trigger = "CREATE TRIGGER counter_poison BEFORE INSERT ON counters BEGIN INSERT INTO counter_poison_guard(name) VALUES(NEW.name); END"
	}
	_, err := raw.Exec(trigger)
	require.NoError(t, err)
	p := uncachedCounterProcessor(t, repo)
	poison := counterTestEvent("poison", 4)
	poison.Bumps[0].Name = "poison"
	events := []data.CounterBumpedDTO{counterTestEvent("healthy-one", 2), poison, counterTestEvent("healthy-two", 3)}
	errs := runCounterCopies(t.Context(), []*CounterProcessor{p}, events)
	for i, err := range errs {
		if i == 1 {
			require.ErrorIs(t, err, ErrInvalidInput)
			continue
		}
		require.NoError(t, err)
	}
	require.EqualValues(t, 5, counterTestValue(t, raw, "deaths"))
	assertCounterReceipts(t, raw, 2)
	_, err = raw.Exec("DROP TRIGGER counter_poison")
	require.NoError(t, err)
	require.NoError(t, p.Process(t.Context(), poison))
	assertCounterReceipts(t, raw, 3)
}

func counterBinaryIdentity(t *testing.T, repo *Loyalty, raw *sql.DB) {
	p := uncachedCounterProcessor(t, repo)
	ids := []string{"batch", "Batch", "batch ", "0123", "123", "1e3", strings.Repeat("z", 256)}
	for _, id := range ids {
		event := counterTestEvent(id, 1)
		require.NoError(t, p.Process(t.Context(), event))
		require.NoError(t, p.Process(t.Context(), event))
	}
	require.EqualValues(t, len(ids), counterTestValue(t, raw, "deaths"))
	assertCounterReceipts(t, raw, len(ids))
}

func counterReceiptPruning(t *testing.T, repo *Loyalty, raw *sql.DB) {
	p := uncachedCounterProcessor(t, repo)
	require.NoError(t, p.Process(t.Context(), counterTestEvent("retained", 2)))
	require.Equal(t, 2*time.Hour, counterReceiptRetention())
	cutoff := time.Now().Add(-counterReceiptRetention()).Unix()
	seedExpiredCounterReceipts(t, raw, cutoff-60)
	_, err := raw.Exec("UPDATE loyalty_counter_receipts SET created_at=? WHERE batch_id=?", cutoff+60, []byte(counterIdentity(counterTestEvent("retained", 2)).batch))
	require.NoError(t, err)
	require.NoError(t, repo.PruneCounterReceipts(t.Context()))
	assertCounterReceipts(t, raw, 1)
	seedExpiredCounterReceipts(t, raw, cutoff-60)
	repo.ticker.Reset(10 * time.Millisecond)
	require.Eventually(t, func() bool {
		var count int
		err := raw.QueryRow("SELECT COUNT(*) FROM loyalty_counter_receipts").Scan(&count)
		return err == nil && count == 1
	}, time.Second, 10*time.Millisecond)
	repo.ticker.Reset(flushInterval)
	require.NoError(t, p.Process(t.Context(), counterTestEvent("retained", 2)))
	require.EqualValues(t, 2, counterTestValue(t, raw, "deaths"))
	var batch, digest, owner []byte
	require.NoError(t, raw.QueryRow("SELECT batch_id,payload_hash,admission_id FROM loyalty_counter_receipts").Scan(&batch, &digest, &owner))
	require.Equal(t, []int{64, 32, 16}, []int{len(batch), len(digest), len(owner)})
}

func seedExpiredCounterReceipts(t *testing.T, raw *sql.DB, createdAt int64) {
	t.Helper()
	tx, err := raw.BeginTx(t.Context(), nil)
	require.NoError(t, err)
	defer tx.Rollback()
	for i := range watchHistoryPruneBatch + 1 {
		_, err := tx.ExecContext(t.Context(), "INSERT INTO loyalty_counter_receipts (user_id,batch_id,payload_hash,admission_id,created_at) VALUES (1,?,?,?,?)", []byte(fmt.Sprint(i)), make([]byte, 32), make([]byte, 16), createdAt)
		require.NoError(t, err)
	}
	require.NoError(t, tx.Commit())
}

func counterLargePayload(t *testing.T, repo *Loyalty, raw *sql.DB) {
	p := uncachedCounterProcessor(t, repo)
	event := counterTestEvent("large-payload", 1)
	event.Bumps = nil
	identity := strings.Repeat("é", maxCounterName)
	for i := range 1100 {
		event.Bumps = append(event.Bumps, data.CounterBumpEntry{Name: "hugs", Scope: data.CounterScopeViewerCommand, Command: identity, ViewerID: uint64(i + 1), ViewerLogin: identity, ViewerName: identity, Delta: 1})
	}
	for range 2 {
		require.NoError(t, p.Process(t.Context(), event))
	}
	var total int
	require.NoError(t, raw.QueryRow("SELECT SUM(value) FROM counter_entries").Scan(&total))
	require.Equal(t, 1100, total)
	assertCounterReceipts(t, raw, 1)
}

func TestCounterMySQLRetriesEntireReceiptTransaction(t *testing.T) {
	for _, number := range []int{1205, 1213} {
		t.Run(fmt.Sprint(number), func(t *testing.T) {
			raw := testdb.MySQL(t, "loyalty_counter_retry_")
			raw.SetMaxOpenConns(1)
			repo := mysqlReplica(t, raw)
			trigger := fmt.Sprintf("CREATE TRIGGER counter_retry BEFORE INSERT ON counters FOR EACH ROW BEGIN SET @counter_attempts=COALESCE(@counter_attempts,0)+1; IF @counter_attempts=1 THEN SIGNAL SQLSTATE '40001' SET MYSQL_ERRNO=%d, MESSAGE_TEXT='retry entire counter transaction'; END IF; END", number)
			_, err := raw.Exec(trigger)
			require.NoError(t, err)
			p := uncachedCounterProcessor(t, repo)
			require.NoError(t, p.Process(t.Context(), counterTestEvent("retry-transaction", 5)))
			require.EqualValues(t, 5, counterTestValue(t, raw, "deaths"))
			assertCounterReceipts(t, raw, 1)
		})
	}
}

func assertCounterReceipts(t *testing.T, raw *sql.DB, expected int) {
	t.Helper()
	var count int
	require.NoError(t, raw.QueryRow("SELECT COUNT(*) FROM loyalty_counter_receipts").Scan(&count))
	require.Equal(t, expected, count)
}
