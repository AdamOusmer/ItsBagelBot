// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository

import (
	"crypto/sha256"
	"fmt"
	"testing"

	"ItsBagelBot/internal/domain/event/data"
	"ItsBagelBot/internal/testdb"
	"ItsBagelBot/pkg/db"
	pkgvalkey "ItsBagelBot/pkg/valkey"
	"github.com/stretchr/testify/require"
)

func TestCounterReceiptRollingCompatibility(t *testing.T) {
	for _, direction := range []string{"old-to-new", "new-to-old"} {
		t.Run(direction, func(t *testing.T) {
			repo, raw := counterTestRepo(t)
			cache := counterRealValkey(t)
			p := counterTestProcessor(t, repo, cache)
			event := counterTestEvent("rollout", 2)
			key := fmt.Sprintf("loyalty:counter:receipt:1:%x", sha256.Sum256([]byte(event.BatchID)))
			lease := pkgvalkey.MarkedLease{Key: key, Done: "done"}
			require.Equal(t, key, counterReceiptKey(event))
			if direction == "old-to-new" {
				seedLegacyCounterReceipt(t, p, lease, event)
			}
			require.NoError(t, p.Process(t.Context(), event))
			status, err := pkgvalkey.RunScripts(t.Context(), cache, []pkgvalkey.ScriptCall{lease.ClaimCall("old-owner", counterPendingTTL)})[0].ToInt64()
			require.NoError(t, err)
			require.Equal(t, pkgvalkey.LeaseDone, status)
			require.EqualValues(t, 2, counterTestValue(t, raw, "deaths"))
		})
	}
}

func seedLegacyCounterReceipt(t *testing.T, p *CounterProcessor, lease pkgvalkey.MarkedLease, event data.CounterBumpedDTO) {
	t.Helper()
	require.NoError(t, p.repo.persistCounterBatch(t.Context(), []data.CounterBumpedDTO{event}))
	_, err := p.repo.sqldb.ExecContext(t.Context(), "DELETE FROM loyalty_counter_receipts")
	require.NoError(t, err)
	for _, call := range []pkgvalkey.ScriptCall{lease.ClaimCall("old-owner", counterPendingTTL), lease.FinishCall("old-owner", counterCompletedTTL)} {
		status, err := pkgvalkey.RunScripts(t.Context(), p.client, []pkgvalkey.ScriptCall{call})[0].ToInt64()
		require.NoError(t, err)
		require.EqualValues(t, 1, status)
	}
}

func TestCounterMySQLTransientBatchDoesNotSplit(t *testing.T) {
	for _, number := range []int{1205, 1213, 1644} {
		t.Run(fmt.Sprint(number), func(t *testing.T) {
			raw := testdb.MySQL(t, "loyalty_counter_outage_")
			raw.SetMaxOpenConns(1)
			repo := mysqlReplica(t, raw)
			trigger := fmt.Sprintf("CREATE TRIGGER counter_unavailable BEFORE INSERT ON counters FOR EACH ROW BEGIN SET @counter_attempts=COALESCE(@counter_attempts,0)+1; SIGNAL SQLSTATE '45000' SET MYSQL_ERRNO=%d, MESSAGE_TEXT='SQL unavailable'; END", number)
			_, err := raw.Exec(trigger)
			require.NoError(t, err)
			p := &CounterProcessor{repo: repo}
			deliveries := make([]*counterDelivery, counterBatchMessages)
			for i := range deliveries {
				deliveries[i] = &counterDelivery{request: &counterRequest{dto: counterTestEvent(fmt.Sprint(i), 1)}}
			}
			p.persistDeliveries(t.Context(), deliveries)
			for _, delivery := range deliveries {
				require.ErrorContains(t, delivery.err, "SQL unavailable")
			}
			var attempts int
			require.NoError(t, raw.QueryRow("SELECT @counter_attempts").Scan(&attempts))
			expected := db.TxAttempts
			if number == 1644 {
				expected = 1
			}
			require.Equal(t, expected, attempts)
			assertCounterReceipts(t, raw, 0)
		})
	}
}
