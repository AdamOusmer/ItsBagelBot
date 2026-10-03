// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository_test

import (
	"context"
	"testing"
	"time"

	"ItsBagelBot/app/db/notifications/ent"
	"ItsBagelBot/app/db/notifications/ent/enttest"
	"ItsBagelBot/app/db/notifications/ent/notification"
	"ItsBagelBot/app/db/notifications/ent/notificationread"
	"ItsBagelBot/app/db/notifications/repository"

	"ItsBagelBot/internal/testdb"

	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	alice = uint64(1001)
	bob   = uint64(2002)
)

type fixture struct {
	repo   *repository.Notifications
	client *ent.Client
	ctx    context.Context
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	client := enttest.Open(t, testdb.Driver, testdb.MemDSN(testdb.Name(t.Name())))
	t.Cleanup(func() { _ = client.Close() })
	return &fixture{repo: repository.New(client), client: client, ctx: context.Background()}
}

func (f *fixture) send(t *testing.T, edit func(*repository.CreateParams)) *ent.Notification {
	t.Helper()
	params := repository.CreateParams{Scope: notification.ScopeBroadcast, Title: "Heads up", Body: "Body", Level: notification.LevelInfo, CreatedBy: 1, CreatedByLogin: "itsmavey"}
	if edit != nil {
		edit(&params)
	}
	row, _, err := f.repo.Create(f.ctx, params)
	require.NoError(t, err)
	return row
}

func (f *fixture) titlesFor(t *testing.T, userID uint64) []string {
	t.Helper()
	rows, _, err := f.repo.ListForUser(f.ctx, userID, repository.UserListLimit)
	require.NoError(t, err)
	titles := make([]string, 0, len(rows))
	for _, row := range rows {
		titles = append(titles, row.Title)
	}
	return titles
}

func TestListForUserVisibility(t *testing.T) {
	expired, live := time.Now().Add(-time.Hour), time.Now().Add(time.Hour)
	for _, tc := range []struct {
		name string
		send []func(*repository.CreateParams)
		want map[uint64][]string
	}{
		{
			name: "broadcast reaches every user",
			send: []func(*repository.CreateParams){func(p *repository.CreateParams) { p.Title = "Maintenance" }},
			want: map[uint64][]string{alice: {"Maintenance"}, bob: {"Maintenance"}},
		},
		{
			name: "direct notification does not reach other users",
			send: []func(*repository.CreateParams){func(p *repository.CreateParams) {
				target := alice
				p.Scope, p.TargetUserID, p.Title = notification.ScopeDirect, &target, "Welcome"
			}},
			want: map[uint64][]string{alice: {"Welcome"}, bob: {}},
		},
		{
			name: "expired notification is excluded",
			send: []func(*repository.CreateParams){
				func(p *repository.CreateParams) { p.Title, p.ExpiresAt = "Expired", &expired },
				func(p *repository.CreateParams) { p.Title, p.ExpiresAt = "Still live", &live },
			},
			want: map[uint64][]string{alice: {"Still live"}},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t)
			for _, edit := range tc.send {
				f.send(t, edit)
			}
			for userID, titles := range tc.want {
				assert.Equal(t, titles, f.titlesFor(t, userID), "user %d", userID)
			}
		})
	}
}

func TestMarkRead(t *testing.T) {
	for _, tc := range []struct {
		name       string
		cutoff     time.Duration
		wantListed bool
	}{
		{"is idempotent and per user", time.Hour, true},
		{"cutoff hides the notification after expiry", -time.Minute, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t)
			row := f.send(t, nil)
			cutoff := time.Now().Add(tc.cutoff)
			require.NoError(t, f.repo.MarkRead(f.ctx, row.ID, alice, cutoff))
			require.NoError(t, f.repo.MarkRead(f.ctx, row.ID, alice, cutoff), "repeat mark-read must not error")

			rows, read, err := f.repo.ListForUser(f.ctx, alice, repository.UserListLimit)
			require.NoError(t, err)
			assert.Equal(t, tc.wantListed, len(rows) == 1)
			assert.Equal(t, tc.wantListed, read[row.ID])

			rows, read, err = f.repo.ListForUser(f.ctx, bob, repository.UserListLimit)
			require.NoError(t, err)
			require.Len(t, rows, 1, "another user's cutoff must not hide it")
			assert.False(t, read[row.ID], "another user's read state must not leak")
		})
	}
}

func TestMarkPeekedAcknowledgesWithoutClobberingFullRead(t *testing.T) {
	f := newFixture(t)
	unread := f.send(t, nil)
	read := f.send(t, nil)
	shortCutoff := time.Now().Add(time.Minute)
	require.NoError(t, f.repo.MarkRead(f.ctx, read.ID, alice, shortCutoff))

	peeked, err := f.repo.MarkPeeked(f.ctx, alice, time.Now().Add(24*time.Hour))
	require.NoError(t, err)
	assert.Equal(t, 1, peeked, "only the unread notification is newly peeked")

	_, acknowledged, err := f.repo.ListForUser(f.ctx, alice, repository.UserListLimit)
	require.NoError(t, err)
	assert.Equal(t, map[int]bool{unread.ID: true, read.ID: true}, acknowledged)

	receipt := f.client.NotificationRead.Query().
		Where(notificationread.UserIDEQ(alice), notificationread.HasNotificationWith(notification.IDEQ(read.ID))).
		OnlyX(f.ctx)
	assert.WithinDuration(t, shortCutoff, *receipt.ExpiresAt, time.Second, "peek must not extend a full-read cutoff")

	peeked, err = f.repo.MarkPeeked(f.ctx, alice, time.Now().Add(24*time.Hour))
	require.NoError(t, err)
	assert.Zero(t, peeked)
}

func TestDeleteCascadesReads(t *testing.T) {
	f := newFixture(t)
	row := f.send(t, nil)
	require.NoError(t, f.repo.MarkRead(f.ctx, row.ID, alice, time.Now().Add(time.Hour)))

	require.NoError(t, f.repo.Delete(f.ctx, row.ID))

	assert.Zero(t, f.client.NotificationRead.Query().CountX(f.ctx), "cascade must remove read receipts")
	admin, err := f.repo.ListForAdmin(f.ctx, repository.AdminPageSize, 0)
	require.NoError(t, err)
	assert.Empty(t, admin)
}

func TestDeleteExpiredSweepsGloballyExpired(t *testing.T) {
	f := newFixture(t)
	past, future := time.Now().Add(-time.Hour), time.Now().Add(time.Hour)
	dead := f.send(t, func(p *repository.CreateParams) { p.ExpiresAt = &past })
	require.NoError(t, f.repo.MarkRead(f.ctx, dead.ID, alice, time.Now().Add(time.Hour)))
	f.send(t, func(p *repository.CreateParams) { p.ExpiresAt = &future })
	f.send(t, nil)

	removed, err := f.repo.DeleteExpired(f.ctx, time.Now())
	require.NoError(t, err)
	assert.Equal(t, 1, removed, "only the globally-expired notification is swept")
	assert.Equal(t, 2, f.client.Notification.Query().CountX(f.ctx))
	assert.Zero(t, f.client.NotificationRead.Query().CountX(f.ctx), "swept notification's reads cascade")
}

func TestCreateIsIdempotentByRequestID(t *testing.T) {
	f := newFixture(t)
	params := repository.CreateParams{RequestID: "send-123", Scope: notification.ScopeBroadcast, Title: "Once", Body: "Body", Level: notification.LevelInfo, CreatedBy: 1, CreatedByLogin: "itsmavey"}

	first, created, err := f.repo.Create(f.ctx, params)
	require.NoError(t, err)
	assert.True(t, created)

	duplicate, created, err := f.repo.Create(f.ctx, params)
	require.NoError(t, err)
	assert.False(t, created)
	assert.Equal(t, first.ID, duplicate.ID)
	assert.Equal(t, 1, f.client.Notification.Query().CountX(f.ctx))
}
