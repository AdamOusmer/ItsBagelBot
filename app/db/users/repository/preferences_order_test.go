// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"ItsBagelBot/app/db/users/ent"
	"ItsBagelBot/app/db/users/ent/enttest"
	"ItsBagelBot/app/db/users/ent/user"
	"ItsBagelBot/internal/testdb"
	"ItsBagelBot/pkg/batch"
	"ItsBagelBot/pkg/bus/bustest"
	entsql "entgo.io/ent/dialect/sql"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestPreferenceFlushLocksUsersInAscendingOrder(t *testing.T) {
	for _, backend := range []string{"sqlite", "mysql"} {
		t.Run(backend, func(t *testing.T) {
			preferenceLockOrder(t, preferenceOrderClient(t, backend))
		})
	}
}

func preferenceOrderClient(t *testing.T, backend string) *ent.Client {
	t.Helper()
	if backend == "sqlite" {
		return testdb.Open(t, "preferenceorder", func(d, dsn string) *ent.Client { return enttest.Open(t, d, dsn) })
	}
	client := ent.NewClient(ent.Driver(entsql.OpenDB("mysql", testdb.MySQL(t, "prefs_order_"))))
	require.NoError(t, client.Schema.Create(t.Context()))
	return client
}

func preferenceLockOrder(t *testing.T, client *ent.Client) {
	t.Helper()
	repo := NewUsers(UsersConfig{Client: client, Pub: bustest.NewPublisher(), Log: zap.NewNop()})
	t.Cleanup(func() { repo.Close(context.Background()) })
	require.NoError(t, repo.preferences.Close(t.Context()))
	queued := make(chan struct{}, 4)
	repo.preferences = batch.NewGroup(batch.GroupConfig[prefWrite]{
		Window: time.Hour, MaxRows: 2, MaxBytes: 256 * 1024,
		Size: func(w prefWrite) int { queued <- struct{}{}; return preferenceBytes(w) },
		Key:  preferenceKey, Timeout: time.Second, AfterCommit: repo.afterPreferences,
	}, repo.flushPrefs)
	for _, id := range []uint64{7101, 7102, 7103} {
		require.NoError(t, repo.Register(t.Context(), Registration{ID: id, Username: fmt.Sprint(id), DisplayName: "Viewer", Email: fmt.Sprintf("%d@example.invalid", id)}))
	}
	entered, release := make(chan struct{}), make(chan struct{})
	var order []uint64
	client.User.Use(preferenceOrderHook(&order, entered, release))
	var writes sync.WaitGroup
	writes.Go(func() { assert.NoError(t, repo.SetLocale(t.Context(), 7103, "fr")) })
	writes.Go(func() { assert.NoError(t, repo.SetCustomCursor(t.Context(), 7103, false)) })
	<-entered
	<-queued
	<-queued
	for _, id := range []uint64{7102, 7101} {
		writes.Go(func() { assert.NoError(t, repo.SetLocale(t.Context(), id, "fr")) })
	}
	<-queued
	<-queued
	close(release)
	writes.Wait()
	require.Equal(t, []uint64{7101, 7102}, order)
}

func preferenceOrderHook(order *[]uint64, entered, release chan struct{}) ent.Hook {
	return func(next ent.Mutator) ent.Mutator {
		return ent.MutateFunc(func(ctx context.Context, m ent.Mutation) (ent.Value, error) {
			id, _ := m.(*ent.UserMutation).ID()
			if id == 7103 {
				close(entered)
				<-release
				return next.Mutate(ctx, m)
			}
			if _, ok := m.Field(user.FieldLocale); ok {
				*order = append(*order, id)
			}
			return next.Mutate(ctx, m)
		})
	}
}
