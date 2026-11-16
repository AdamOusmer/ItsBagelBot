// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository_test

import (
	"context"
	"testing"
	"time"

	"ItsBagelBot/app/db/notifications/ent"
	"ItsBagelBot/pkg/db"

	"github.com/go-sql-driver/mysql"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAdminDeleteHandlesConcurrentChanges(t *testing.T) {
	for _, dialect := range []string{"sqlite", "mysql"} {
		for _, change := range []string{"read", "delete", "missing"} {
			t.Run(dialect+"/"+change, func(t *testing.T) {
				checkAdminDeleteChange(t, cleanupFixture(t, dialect), change)
			})
		}
	}
}

func checkAdminDeleteChange(t *testing.T, f *fixture, change string) {
	t.Helper()
	row := f.send(t, nil)
	if change == "missing" {
		require.NoError(t, f.repo.Delete(f.ctx, row.ID))
	}
	attempts := 0
	f.client.Notification.Use(func(next ent.Mutator) ent.Mutator {
		return ent.MutateFunc(func(ctx context.Context, m ent.Mutation) (ent.Value, error) {
			attempts++
			if attempts == 1 {
				return raceAdminDelete(ctx, next, m, change)
			}
			return next.Mutate(ctx, m)
		})
	})
	err := f.repo.Delete(f.ctx, row.ID)
	assert.Equal(t, change == "missing", ent.IsNotFound(err), "%v", err)
	if change != "missing" {
		require.NoError(t, err)
	}
	assert.Zero(t, f.client.Notification.Query().CountX(f.ctx))
	assert.Zero(t, f.client.NotificationRead.Query().CountX(f.ctx))
	if change == "read" {
		assert.Equal(t, 2, attempts)
	}
}

func raceAdminDelete(ctx context.Context, next ent.Mutator, m ent.Mutation, change string) (ent.Value, error) {
	mutation := m.(*ent.NotificationMutation)
	ids, err := mutation.IDs(ctx)
	if err != nil {
		return nil, err
	}
	if change == "read" {
		err = mutation.Client().NotificationRead.Create().SetNotificationID(ids[0]).SetUserID(alice).Exec(ctx)
	} else {
		_, err = next.Mutate(ctx, m)
	}
	if err != nil {
		return nil, err
	}
	return next.Mutate(ctx, m)
}

func TestAdminDeleteRetriesWholeTransaction(t *testing.T) {
	for _, code := range []uint16{db.MySQLDeadlock, db.MySQLLockWaitTimeout} {
		t.Run((&mysql.MySQLError{Number: code}).Error(), func(t *testing.T) {
			f := cleanupFixture(t, "mysql")
			row := f.send(t, nil)
			seedCleanupReads(t, f, row.ID)
			attempts := 0
			f.client.NotificationRead.Use(func(next ent.Mutator) ent.Mutator {
				return ent.MutateFunc(func(ctx context.Context, m ent.Mutation) (ent.Value, error) {
					attempts++
					if attempts == 2 {
						return nil, &mysql.MySQLError{Number: code}
					}
					return next.Mutate(ctx, m)
				})
			})
			require.NoError(t, f.repo.Delete(f.ctx, row.ID))
			assert.Equal(t, 6, attempts)
			assert.Zero(t, f.client.Notification.Query().CountX(f.ctx))
			assert.Zero(t, f.client.NotificationRead.Query().CountX(f.ctx))
		})
	}
}

func TestAdminDeleteCancellationKeepsReadState(t *testing.T) {
	for _, dialect := range []string{"sqlite", "mysql"} {
		t.Run(dialect, func(t *testing.T) {
			f := cleanupFixture(t, dialect)
			row := f.send(t, nil)
			seedCleanupReads(t, f, row.ID)
			ctx, cancel := context.WithCancel(f.ctx)
			defer cancel()
			attempts := 0
			f.client.NotificationRead.Use(func(next ent.Mutator) ent.Mutator {
				return ent.MutateFunc(func(ctx context.Context, m ent.Mutation) (ent.Value, error) {
					attempts++
					if attempts == 2 {
						cancel()
						return nil, ctx.Err()
					}
					return next.Mutate(ctx, m)
				})
			})
			require.ErrorIs(t, f.repo.Delete(ctx, row.ID), context.Canceled)
			require.Eventually(t, func() bool {
				count, err := f.client.NotificationRead.Query().Count(f.ctx)
				return err == nil && count == 1501
			}, time.Second, time.Millisecond)
			require.Equal(t, 1, f.client.Notification.Query().CountX(f.ctx))
		})
	}
}

func TestAdminDeleteConcurrentRemovalDuringRetry(t *testing.T) {
	for _, dialect := range []string{"sqlite", "mysql"} {
		t.Run(dialect, func(t *testing.T) {
			f := cleanupFixture(t, dialect)
			row := f.send(t, nil)
			require.NoError(t, f.repo.MarkRead(f.ctx, row.ID, alice, time.Now().Add(time.Hour)))
			first := true
			f.client.NotificationRead.Use(func(next ent.Mutator) ent.Mutator {
				return ent.MutateFunc(func(ctx context.Context, m ent.Mutation) (ent.Value, error) {
					if !first {
						return next.Mutate(ctx, m)
					}
					first = false
					tx, err := m.(*ent.NotificationReadMutation).Tx()
					require.NoError(t, err)
					tx.OnRollback(removeNotificationAfterRollback(f, row.ID))
					return nil, &mysql.MySQLError{Number: db.MySQLDeadlock}
				})
			})
			require.NoError(t, f.repo.Delete(f.ctx, row.ID))
			assert.Zero(t, f.client.Notification.Query().CountX(f.ctx))
		})
	}
}

func removeNotificationAfterRollback(f *fixture, id int) ent.RollbackHook {
	return func(next ent.Rollbacker) ent.Rollbacker {
		return ent.RollbackFunc(func(ctx context.Context, tx *ent.Tx) error {
			if err := next.Rollback(ctx, tx); err != nil {
				return err
			}
			return f.repo.Delete(ctx, id)
		})
	}
}
