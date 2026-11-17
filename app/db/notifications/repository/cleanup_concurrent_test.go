// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository_test

import (
	"context"
	"strconv"
	"testing"
	"time"

	"ItsBagelBot/app/db/notifications/ent"
	"ItsBagelBot/app/db/notifications/ent/notification"
	"ItsBagelBot/app/db/notifications/repository"
	"ItsBagelBot/pkg/db"

	"entgo.io/ent/dialect/sql"
	"github.com/go-sql-driver/mysql"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func runConnectionCleanup(ctx context.Context, f *fixture, operation string, p repository.ConnectionParams) error {
	if operation == "retract" {
		_, err := f.repo.RetractConnection(ctx, p, time.Now())
		return err
	}
	return f.repo.EraseConnection(ctx, p.AccountID, p.Incarnation, time.Now())
}

func TestConnectionCleanupSeesAcknowledgmentAfterSnapshot(t *testing.T) {
	for _, operation := range []string{"retract", "erase"} {
		t.Run(operation, func(t *testing.T) {
			f := cleanupFixture(t, "mysql")
			p := connection(time.Now())
			row, err := f.repo.CreateConnection(f.ctx, p, time.Now())
			require.NoError(t, err)
			acknowledged := false
			f.client.ConnectionGuard.Intercept(ent.InterceptFunc(func(next ent.Querier) ent.Querier {
				return ent.QuerierFunc(func(ctx context.Context, q ent.Query) (ent.Value, error) {
					value, err := next.Query(ctx, q)
					if !acknowledged {
						acknowledged = true
						require.NoError(t, f.repo.MarkRead(f.ctx, row.NotificationID, alice, time.Now().Add(time.Hour)))
					}
					return value, err
				})
			}))
			ctx, cancel := context.WithTimeout(f.ctx, 2*time.Second)
			defer cancel()
			require.NoError(t, runConnectionCleanup(ctx, f, operation, p))
			assert.Equal(t, struct{ Notifications, Reads int }{}, struct{ Notifications, Reads int }{
				f.client.Notification.Query().CountX(f.ctx), f.client.NotificationRead.Query().CountX(f.ctx),
			})
			assert.True(t, acknowledged)
		})
	}
}

func TestConnectionCleanupLocksParentBeforeDraining(t *testing.T) {
	for _, operation := range []string{"retract", "erase"} {
		t.Run(operation, func(t *testing.T) {
			f := cleanupFixture(t, "mysql")
			p := connection(time.Now())
			row, err := f.repo.CreateConnection(f.ctx, p, time.Now())
			require.NoError(t, err)
			checked := false
			f.client.NotificationRead.Intercept(ent.InterceptFunc(func(next ent.Querier) ent.Querier {
				return ent.QuerierFunc(func(ctx context.Context, q ent.Query) (ent.Value, error) {
					if !checked {
						checked = true
						assertNotificationLocked(t, f, row.NotificationID)
					}
					return next.Query(ctx, q)
				})
			}))
			require.NoError(t, runConnectionCleanup(f.ctx, f, operation, p))
			assert.True(t, checked)
		})
	}
}

func assertNotificationLocked(t *testing.T, f *fixture, id int) {
	t.Helper()
	tx, err := f.client.Tx(f.ctx)
	require.NoError(t, err)
	_, err = tx.Notification.Query().Where(notification.IDEQ(id), func(s *sql.Selector) {
		s.ForUpdate(sql.WithLockAction(sql.NoWait))
	}).Only(f.ctx)
	var locked *mysql.MySQLError
	require.ErrorAs(t, err, &locked)
	assert.EqualValues(t, 3572, locked.Number)
	require.NoError(t, tx.Rollback())
}

func TestConnectionCleanupRetriesWholeTransaction(t *testing.T) {
	for _, operation := range []string{"retract", "erase"} {
		for _, code := range []uint16{db.MySQLDeadlock, db.MySQLLockWaitTimeout} {
			t.Run(operation+"/"+(&mysql.MySQLError{Number: code}).Error(), func(t *testing.T) {
				checkConnectionCleanupRetry(t, operation, code)
			})
		}
	}
}

func checkConnectionCleanupRetry(t *testing.T, operation string, code uint16) {
	t.Helper()
	f := cleanupFixture(t, "mysql")
	p := connection(time.Now())
	row, err := f.repo.CreateConnection(f.ctx, p, time.Now())
	require.NoError(t, err)
	seedCleanupReads(t, f, row.NotificationID)
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
	require.NoError(t, runConnectionCleanup(f.ctx, f, operation, p))
	assert.Equal(t, struct{ Attempts, Notifications, Reads int }{6, 0, 0}, struct{ Attempts, Notifications, Reads int }{
		attempts, f.client.Notification.Query().CountX(f.ctx), f.client.NotificationRead.Query().CountX(f.ctx),
	})
}

type stalledCleanup struct {
	operation string
	parent    bool
}

func TestCleanupStopsWhenDeleteMakesNoProgress(t *testing.T) {
	for _, dialect := range []string{"sqlite", "mysql"} {
		for _, tc := range []stalledCleanup{
			{operation: "admin"}, {operation: "erasure"}, {operation: "expired"},
			{operation: "admin", parent: true}, {operation: "erasure", parent: true},
		} {
			t.Run(dialect+"/"+tc.operation+"/parent="+strconv.FormatBool(tc.parent), func(t *testing.T) {
				checkCleanupNoProgress(t, cleanupFixture(t, dialect), tc)
			})
		}
	}
}

func checkCleanupNoProgress(t *testing.T, f *fixture, tc stalledCleanup) {
	t.Helper()
	past := time.Now().Add(-time.Hour)
	row := f.send(t, func(p *repository.CreateParams) {
		p.ExpiresAt, p.TargetUserID, p.Incarnation = &past, &[]uint64{alice}[0], "123"
	})
	require.NoError(t, f.repo.MarkRead(f.ctx, row.ID, alice, time.Now().Add(time.Hour)))
	attempts := 0
	hook := func(ent.Mutator) ent.Mutator {
		return ent.MutateFunc(func(context.Context, ent.Mutation) (ent.Value, error) {
			attempts++
			return 0, nil
		})
	}
	if tc.parent {
		f.client.Notification.Use(hook)
	} else {
		f.client.NotificationRead.Use(hook)
	}
	require.ErrorContains(t, runNotificationCleanup(f, tc.operation, row.ID), "made no progress")
	assert.Equal(t, struct{ Attempts, Notifications, Reads int }{1, 1, 1}, struct{ Attempts, Notifications, Reads int }{
		attempts, f.client.Notification.Query().CountX(f.ctx), f.client.NotificationRead.Query().CountX(f.ctx),
	})
}
