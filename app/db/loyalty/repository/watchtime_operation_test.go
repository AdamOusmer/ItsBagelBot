// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository_test

import (
	"database/sql"
	"testing"

	loyaltyrepo "ItsBagelBot/app/db/loyalty/repository"
	"github.com/stretchr/testify/require"
)

func TestWatchOperationMigrationPreservesCompletedReplay(t *testing.T) {
	for dialect, open := range map[string]func(*testing.T) (*loyaltyrepo.Loyalty, *sql.DB){
		"sqlite": sqliteRetentionRepo,
		"mysql":  mysqlRetentionRepo,
	} {
		t.Run(dialect, func(t *testing.T) {
			repo, raw := open(t)
			a := bulkWatchAward(1, 1000)
			require.NoError(t, repo.ApplyWatchAward(t.Context(), a))
			_, err := raw.ExecContext(t.Context(), "ALTER TABLE loyalty_watch_operations DROP COLUMN completed")
			require.NoError(t, err)
			for range 2 {
				require.NoError(t, repo.EnsureWatchSchema(t.Context()))
				require.NoError(t, repo.ApplyWatchAward(t.Context(), a))
				require.Equal(t, watchTotals{1000, 10000, 300000, 1, 1000}, (watchBulkFixture{repo, raw}).totals(t))
			}
		})
	}
}
