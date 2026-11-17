// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository_test

import (
	"context"
	"testing"
	"time"

	"ItsBagelBot/app/db/notifications/ent"
	"ItsBagelBot/app/db/notifications/ent/notificationread"
	"ItsBagelBot/app/db/notifications/repository"
	"ItsBagelBot/internal/testdb"

	entsql "entgo.io/ent/dialect/sql"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func cleanupFixture(t *testing.T, dialect string) *fixture {
	t.Helper()
	if dialect == "sqlite" {
		return newFixture(t)
	}
	raw := testdb.MySQL(t, "notification_cleanup_")
	client := ent.NewClient(ent.Driver(entsql.OpenDB("mysql", raw)))
	require.NoError(t, repository.MigrateSchema(t.Context(), client))
	return &fixture{repo: repository.New(client), client: client, ctx: t.Context()}
}

func seedCleanupReads(t *testing.T, f *fixture, id int) {
	t.Helper()
	for start := 0; start < 1501; start += 100 {
		var builders []*ent.NotificationReadCreate
		for i := start; i < min(start+100, 1501); i++ {
			builders = append(builders, f.client.NotificationRead.Create().SetNotificationID(id).SetUserID(uint64(i+1)))
		}
		require.NoError(t, f.client.NotificationRead.CreateBulk(builders...).Exec(f.ctx))
	}
}

func TestCleanupBoundsReceiptDeletesAndResumes(t *testing.T) {
	for _, dialect := range []string{"sqlite", "mysql"} {
		for _, operation := range []string{"expired", "admin", "erasure"} {
			t.Run(dialect+"/"+operation, func(t *testing.T) {
				checkCleanupResume(t, cleanupFixture(t, dialect), operation)
			})
		}
	}
}

func checkCleanupResume(t *testing.T, f *fixture, operation string) {
	t.Helper()
	now := time.Now()
	past := now.Add(-time.Hour)
	row := f.send(t, func(p *repository.CreateParams) {
		p.ExpiresAt, p.TargetUserID, p.Incarnation = &past, &[]uint64{alice}[0], "123"
	})
	seedCleanupReads(t, f, row.ID)
	attempts := 0
	f.client.NotificationRead.Use(func(next ent.Mutator) ent.Mutator {
		return ent.MutateFunc(func(ctx context.Context, m ent.Mutation) (ent.Value, error) {
			attempts++
			if attempts == 2 {
				return nil, assert.AnError
			}
			result, err := next.Mutate(ctx, m)
			if err == nil {
				require.LessOrEqual(t, result.(int), repository.CleanupBatchSize)
			}
			return result, err
		})
	})
	cleanup := func() error { return runNotificationCleanup(f, operation, row.ID) }
	require.ErrorIs(t, cleanup(), assert.AnError)
	wantReads := 1001
	if operation != "expired" {
		wantReads = 1501
	}
	require.Equal(t, wantReads, f.client.NotificationRead.Query().CountX(f.ctx))
	require.Equal(t, 1, f.client.Notification.Query().CountX(f.ctx))

	require.NoError(t, cleanup())
	require.Zero(t, f.client.NotificationRead.Query().CountX(f.ctx))
	require.Zero(t, f.client.Notification.Query().CountX(f.ctx))
}

func runNotificationCleanup(f *fixture, operation string, id int) error {
	switch operation {
	case "expired":
		_, _, err := f.repo.DeleteExpired(f.ctx, time.Now())
		return err
	case "admin":
		return f.repo.Delete(f.ctx, id)
	default:
		return f.repo.EraseConnection(f.ctx, alice, "123", time.Now())
	}
}

func TestCleanupNeverCascadesConcurrentAcknowledgments(t *testing.T) {
	for _, dialect := range []string{"sqlite", "mysql"} {
		t.Run(dialect, func(t *testing.T) {
			f := cleanupFixture(t, dialect)
			past := time.Now().Add(-time.Hour)
			row := f.send(t, func(p *repository.CreateParams) { p.ExpiresAt = &past })
			f.client.Notification.Use(func(next ent.Mutator) ent.Mutator {
				return ent.MutateFunc(func(ctx context.Context, m ent.Mutation) (ent.Value, error) {
					require.NoError(t, f.repo.MarkRead(ctx, row.ID, alice, time.Now().Add(time.Hour)))
					return next.Mutate(ctx, m)
				})
			})
			n, _, err := f.repo.DeleteExpired(f.ctx, time.Now())
			require.NoError(t, err)
			require.Zero(t, n)
			require.Equal(t, 1, f.client.NotificationRead.Query().Where(notificationread.UserIDEQ(alice)).CountX(f.ctx))
		})
	}
}
