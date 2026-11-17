// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository_test

import (
	"testing"
	"time"

	"ItsBagelBot/app/db/notifications/repository"

	"github.com/stretchr/testify/require"
)

func TestCleanupMigrationPreservesCascade(t *testing.T) {
	for _, dialect := range []string{"sqlite", "mysql"} {
		t.Run(dialect, func(t *testing.T) {
			f := cleanupFixture(t, dialect)
			row := f.send(t, nil)
			require.NoError(t, f.repo.MarkRead(f.ctx, row.ID, alice, time.Now().Add(time.Hour)))
			for range 2 {
				require.NoError(t, repository.MigrateSchema(f.ctx, f.client))
			}
			require.NoError(t, f.client.Notification.DeleteOneID(row.ID).Exec(f.ctx))
			require.Zero(t, f.client.NotificationRead.Query().CountX(f.ctx))
		})
	}
}
