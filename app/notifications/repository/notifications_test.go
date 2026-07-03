package repository_test

import (
	"context"
	"testing"
	"time"

	"ItsBagelBot/app/notifications/ent/enttest"
	"ItsBagelBot/app/notifications/ent/notification"
	"ItsBagelBot/app/notifications/ent/notificationread"
	"ItsBagelBot/app/notifications/repository"

	_ "github.com/mattn/go-sqlite3" // Required for the in-memory DB
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBroadcastVisibleToEveryUser(t *testing.T) {
	client := enttest.Open(t, "sqlite3", "file:notifbroadcast?mode=memory&cache=shared&_fk=1")
	t.Cleanup(func() { _ = client.Close() })

	repo := repository.New(client)
	ctx := context.Background()

	_, _, err := repo.Create(ctx, repository.CreateParams{RequestID: "broadcast-1", Scope: notification.ScopeBroadcast, Title: "Maintenance", Body: "Downtime tonight", Level: notification.LevelWarning, CreatedBy: 1, CreatedByLogin: "itsmavey"})
	require.NoError(t, err)

	rows, read, err := repo.ListForUser(ctx, 1001, 50)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	assert.False(t, read[rows[0].ID])

	rows, _, err = repo.ListForUser(ctx, 2002, 50)
	require.NoError(t, err)
	require.Len(t, rows, 1, "broadcast must reach every user")
}

func TestDirectNotificationScopedToTarget(t *testing.T) {
	client := enttest.Open(t, "sqlite3", "file:notifdirect?mode=memory&cache=shared&_fk=1")
	t.Cleanup(func() { _ = client.Close() })

	repo := repository.New(client)
	ctx := context.Background()

	target := uint64(1001)
	_, _, err := repo.Create(ctx, repository.CreateParams{RequestID: "direct-1", Scope: notification.ScopeDirect, TargetUserID: &target, Title: "Welcome", Body: "Thanks for subscribing", Level: notification.LevelInfo, CreatedBy: 1, CreatedByLogin: "itsmavey"})
	require.NoError(t, err)

	rows, _, err := repo.ListForUser(ctx, 1001, 50)
	require.NoError(t, err)
	require.Len(t, rows, 1)

	rows, _, err = repo.ListForUser(ctx, 2002, 50)
	require.NoError(t, err)
	assert.Empty(t, rows, "direct notification must not reach other users")
}

func TestMarkReadIsIdempotentAndPerUser(t *testing.T) {
	client := enttest.Open(t, "sqlite3", "file:notifread?mode=memory&cache=shared&_fk=1")
	t.Cleanup(func() { _ = client.Close() })

	repo := repository.New(client)
	ctx := context.Background()

	row, _, err := repo.Create(ctx, repository.CreateParams{RequestID: "read-1", Scope: notification.ScopeBroadcast, Title: "Heads up", Body: "Body", Level: notification.LevelInfo, CreatedBy: 1, CreatedByLogin: "itsmavey"})
	require.NoError(t, err)

	future := time.Now().Add(time.Hour)
	require.NoError(t, repo.MarkRead(ctx, row.ID, 1001, future))
	require.NoError(t, repo.MarkRead(ctx, row.ID, 1001, future), "repeat mark-read must not error")

	_, read, err := repo.ListForUser(ctx, 1001, 50)
	require.NoError(t, err)
	assert.True(t, read[row.ID])

	_, read, err = repo.ListForUser(ctx, 2002, 50)
	require.NoError(t, err)
	assert.False(t, read[row.ID], "another user's read state must not leak")
}

func TestExpiredNotificationExcluded(t *testing.T) {
	client := enttest.Open(t, "sqlite3", "file:notifexpiry?mode=memory&cache=shared&_fk=1")
	t.Cleanup(func() { _ = client.Close() })

	repo := repository.New(client)
	ctx := context.Background()

	past := time.Now().Add(-time.Hour)
	_, _, err := repo.Create(ctx, repository.CreateParams{RequestID: "expired-1", Scope: notification.ScopeBroadcast, Title: "Expired", Body: "Body", Level: notification.LevelInfo, CreatedBy: 1, CreatedByLogin: "itsmavey", ExpiresAt: &past})
	require.NoError(t, err)

	future := time.Now().Add(time.Hour)
	_, _, err = repo.Create(ctx, repository.CreateParams{RequestID: "live-1", Scope: notification.ScopeBroadcast, Title: "Still live", Body: "Body", Level: notification.LevelInfo, CreatedBy: 1, CreatedByLogin: "itsmavey", ExpiresAt: &future})
	require.NoError(t, err)

	rows, _, err := repo.ListForUser(ctx, 1001, 50)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	assert.Equal(t, "Still live", rows[0].Title)
}

func TestDeleteCascadesReads(t *testing.T) {
	client := enttest.Open(t, "sqlite3", "file:notifdelete?mode=memory&cache=shared&_fk=1")
	t.Cleanup(func() { _ = client.Close() })

	repo := repository.New(client)
	ctx := context.Background()

	row, _, err := repo.Create(ctx, repository.CreateParams{RequestID: "delete-1", Scope: notification.ScopeBroadcast, Title: "Bye", Body: "Body", Level: notification.LevelInfo, CreatedBy: 1, CreatedByLogin: "itsmavey"})
	require.NoError(t, err)
	require.NoError(t, repo.MarkRead(ctx, row.ID, 1001, time.Now().Add(time.Hour)))

	require.NoError(t, repo.Delete(ctx, row.ID))

	assert.Equal(t, 0, client.NotificationRead.Query().CountX(ctx), "cascade must remove read receipts")

	admin, err := repo.ListForAdmin(ctx, 20, 0)
	require.NoError(t, err)
	assert.Empty(t, admin)
}

func TestMarkReadCutoffHidesAfterExpiry(t *testing.T) {
	client := enttest.Open(t, "sqlite3", "file:notifreadcutoff?mode=memory&cache=shared&_fk=1")
	t.Cleanup(func() { _ = client.Close() })

	repo := repository.New(client)
	ctx := context.Background()

	row, _, err := repo.Create(ctx, repository.CreateParams{RequestID: "cutoff-1", Scope: notification.ScopeBroadcast, Title: "Heads up", Body: "Body", Level: notification.LevelInfo, CreatedBy: 1, CreatedByLogin: "itsmavey"})
	require.NoError(t, err)

	// A cutoff already in the past hides the row from that user immediately,
	// while every other user still sees it (per-user, not global, expiry).
	require.NoError(t, repo.MarkRead(ctx, row.ID, 1001, time.Now().Add(-time.Minute)))

	rows, _, err := repo.ListForUser(ctx, 1001, 50)
	require.NoError(t, err)
	assert.Empty(t, rows, "a lapsed per-user cutoff must hide the notification")

	rows, _, err = repo.ListForUser(ctx, 2002, 50)
	require.NoError(t, err)
	require.Len(t, rows, 1, "another user's cutoff must not hide it")
}

func TestMarkPeekedAcknowledgesWithoutClobberingFullRead(t *testing.T) {
	client := enttest.Open(t, "sqlite3", "file:notifpeek?mode=memory&cache=shared&_fk=1")
	t.Cleanup(func() { _ = client.Close() })

	repo := repository.New(client)
	ctx := context.Background()

	a, _, err := repo.Create(ctx, repository.CreateParams{RequestID: "peek-a", Scope: notification.ScopeBroadcast, Title: "A", Body: "Body", Level: notification.LevelInfo, CreatedBy: 1, CreatedByLogin: "itsmavey"})
	require.NoError(t, err)
	b, _, err := repo.Create(ctx, repository.CreateParams{RequestID: "peek-b", Scope: notification.ScopeBroadcast, Title: "B", Body: "Body", Level: notification.LevelInfo, CreatedBy: 1, CreatedByLogin: "itsmavey"})
	require.NoError(t, err)

	// Full-read b first with a short cutoff, then peek. The peek must not extend
	// b's cutoff, and must acknowledge the still-unread a.
	shortCutoff := time.Now().Add(time.Minute)
	require.NoError(t, repo.MarkRead(ctx, b.ID, 1001, shortCutoff))

	peeked, err := repo.MarkPeeked(ctx, 1001, time.Now().Add(24*time.Hour))
	require.NoError(t, err)
	assert.Equal(t, 1, peeked, "only the unread notification is newly peeked")

	_, read, err := repo.ListForUser(ctx, 1001, 50)
	require.NoError(t, err)
	assert.True(t, read[a.ID], "peek acknowledges the unread notification")
	assert.True(t, read[b.ID])

	bRead := client.NotificationRead.Query().
		Where(
			notificationread.UserIDEQ(1001),
			notificationread.HasNotificationWith(notification.IDEQ(b.ID)),
		).OnlyX(ctx)
	assert.WithinDuration(t, shortCutoff, *bRead.ExpiresAt, time.Second,
		"peek must not extend a full-read cutoff")

	// A second peek is a no-op: everything already has a read row.
	peeked, err = repo.MarkPeeked(ctx, 1001, time.Now().Add(24*time.Hour))
	require.NoError(t, err)
	assert.Equal(t, 0, peeked)
}

func TestDeleteExpiredSweepsGloballyExpired(t *testing.T) {
	client := enttest.Open(t, "sqlite3", "file:notifsweep?mode=memory&cache=shared&_fk=1")
	t.Cleanup(func() { _ = client.Close() })

	repo := repository.New(client)
	ctx := context.Background()

	past := time.Now().Add(-time.Hour)
	dead, _, err := repo.Create(ctx, repository.CreateParams{RequestID: "dead-1", Scope: notification.ScopeBroadcast, Title: "Dead", Body: "Body", Level: notification.LevelInfo, CreatedBy: 1, CreatedByLogin: "itsmavey", ExpiresAt: &past})
	require.NoError(t, err)
	require.NoError(t, repo.MarkRead(ctx, dead.ID, 1001, time.Now().Add(time.Hour)))

	future := time.Now().Add(time.Hour)
	_, _, err = repo.Create(ctx, repository.CreateParams{RequestID: "live-1", Scope: notification.ScopeBroadcast, Title: "Live", Body: "Body", Level: notification.LevelInfo, CreatedBy: 1, CreatedByLogin: "itsmavey", ExpiresAt: &future})
	require.NoError(t, err)

	// Never-expires row (nil global cutoff) must survive the sweep.
	_, _, err = repo.Create(ctx, repository.CreateParams{RequestID: "keep-1", Scope: notification.ScopeBroadcast, Title: "Keep", Body: "Body", Level: notification.LevelInfo, CreatedBy: 1, CreatedByLogin: "itsmavey"})
	require.NoError(t, err)

	removed, err := repo.DeleteExpired(ctx, time.Now())
	require.NoError(t, err)
	assert.Equal(t, 1, removed, "only the globally-expired notification is swept")
	assert.Equal(t, 2, client.Notification.Query().CountX(ctx))
	assert.Equal(t, 0, client.NotificationRead.Query().CountX(ctx), "swept notification's reads cascade")
}

func TestCreateIsIdempotentByRequestID(t *testing.T) {
	client := enttest.Open(t, "sqlite3", "file:notifidempotency?mode=memory&cache=shared&_fk=1")
	t.Cleanup(func() { _ = client.Close() })

	repo := repository.New(client)
	ctx := context.Background()

	first, created, err := repo.Create(ctx, repository.CreateParams{RequestID: "send-123", Scope: notification.ScopeBroadcast, Title: "Once", Body: "Body", Level: notification.LevelInfo, CreatedBy: 1, CreatedByLogin: "itsmavey"})
	require.NoError(t, err)
	assert.True(t, created)

	duplicate, created, err := repo.Create(ctx, repository.CreateParams{RequestID: "send-123", Scope: notification.ScopeBroadcast, Title: "Once", Body: "Body", Level: notification.LevelInfo, CreatedBy: 1, CreatedByLogin: "itsmavey"})
	require.NoError(t, err)
	assert.False(t, created)
	assert.Equal(t, first.ID, duplicate.ID)
	assert.Equal(t, 1, client.Notification.Query().CountX(ctx))
}
