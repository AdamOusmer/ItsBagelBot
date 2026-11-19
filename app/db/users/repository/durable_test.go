// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository_test

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"ItsBagelBot/app/db/dbtest"
	"ItsBagelBot/app/db/users/ent"
	"ItsBagelBot/app/db/users/repository"
	"ItsBagelBot/internal/testdb"
	"ItsBagelBot/pkg/bus/bustest"
	entsql "entgo.io/ent/dialect/sql"
	"github.com/go-sql-driver/mysql"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func durableUsers(t *testing.T, mysql bool) (*ent.Client, *repository.Users) {
	t.Helper()
	if !mysql {
		client, _, repo := setup(t)
		return client, repo
	}
	raw := testdb.MySQL(t, "users_durable_")
	client := ent.NewClient(ent.Driver(entsql.OpenDB("mysql", raw)))
	require.NoError(t, client.Schema.Create(t.Context()))
	repo := repository.NewUsers(repository.UsersConfig{Client: client, Packer: dbtest.NewPacker(t), Pub: bustest.NewPublisher(), Log: zap.NewNop()})
	t.Cleanup(func() { repo.Close(context.Background()) })
	return client, repo
}

func TestDashboardPreferenceDurability(t *testing.T) {
	for _, mysql := range []bool{false, true} {
		t.Run(map[bool]string{false: "SQLite", true: "MySQL"}[mysql], func(t *testing.T) {
			for _, test := range []struct {
				name string
				run  func(*testing.T, bool)
			}{
				{"visible", preferencesVisible}, {"same key", preferencesSameKey}, {"poison", preferencesPoison},
				{"shutdown", preferencesShutdown},
				{"caller canceled during commit", preferencesCallerCancellation},
			} {
				t.Run(test.name, func(t *testing.T) { test.run(t, mysql) })
			}
		})
	}
}

func preferencesCallerCancellation(t *testing.T, mysql bool) {
	client, repo := durableUsers(t, mysql)
	require.NoError(t, repo.Register(t.Context(), repository.Registration{ID: 1001, Username: "viewer", DisplayName: "Viewer", Email: "viewer@example.com"}))
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	client.User.Use(func(next ent.Mutator) ent.Mutator {
		return ent.MutateFunc(func(groupCtx context.Context, m ent.Mutation) (ent.Value, error) {
			cancel()
			return next.Mutate(groupCtx, m)
		})
	})
	require.NoError(t, repo.SetLocale(ctx, 1001, "fr"))
	assert.Equal(t, "fr", client.User.GetX(t.Context(), 1001).Locale)
}

func preferencesShutdown(t *testing.T, mysql bool) {
	client, repo := durableUsers(t, mysql)
	require.NoError(t, repo.Register(t.Context(), repository.Registration{ID: 1001, Username: "viewer", DisplayName: "Viewer", Email: "viewer@example.com"}))
	entered, release := make(chan struct{}), make(chan struct{})
	client.User.Use(func(next ent.Mutator) ent.Mutator {
		return ent.MutateFunc(func(ctx context.Context, m ent.Mutation) (ent.Value, error) {
			close(entered)
			<-release
			return next.Mutate(ctx, m)
		})
	})
	written, closed := make(chan error, 1), make(chan struct{})
	go func() { written <- repo.SetLocale(t.Context(), 1001, "fr") }()
	<-entered
	go func() { repo.Close(t.Context()); close(closed) }()
	select {
	case <-closed:
		t.Error("shutdown returned before commit")
	case <-time.After(10 * time.Millisecond):
	}
	close(release)
	require.NoError(t, <-written)
	<-closed
	assert.Equal(t, "fr", client.User.GetX(t.Context(), 1001).Locale)
}

func preferencesVisible(t *testing.T, mysql bool) {
	client, repo := durableUsers(t, mysql)
	require.NoError(t, repo.Register(t.Context(), repository.Registration{ID: 1001, Username: "viewer", DisplayName: "Viewer", Email: "viewer@example.com"}))
	for _, write := range []func() error{
		func() error { return repo.SetLocale(t.Context(), 1001, "fr") },
		func() error { return repo.SetCustomCursor(t.Context(), 1001, false) },
		func() error { return repo.SetOnboarded(t.Context(), 1001, true) },
		func() error { return repo.SetCreatorCode(t.Context(), 1001, "fresh") },
	} {
		require.NoError(t, write())
	}
	row := client.User.GetX(t.Context(), 1001)
	assert.Equal(t, []any{"fr", false, true, "fresh"}, []any{row.Locale, row.CustomCursor, row.Onboarded, *row.CreatorCode})
	view, err := repo.Get(t.Context(), 1001)
	require.NoError(t, err)
	assert.Equal(t, "fr", view.Locale)
}

func preferencesSameKey(t *testing.T, mysql bool) {
	client, repo := durableUsers(t, mysql)
	require.NoError(t, repo.Register(t.Context(), repository.Registration{ID: 1001, Username: "viewer", DisplayName: "Viewer", Email: "viewer@example.com"}))
	var wg sync.WaitGroup
	for range 48 {
		wg.Go(func() { assert.NoError(t, repo.SetLocale(t.Context(), 1001, "de")) })
	}
	wg.Wait()
	assert.Equal(t, "de", client.User.GetX(t.Context(), 1001).Locale)
}

func preferencesPoison(t *testing.T, mysql bool) {
	client, repo := durableUsers(t, mysql)
	require.NoError(t, repo.Register(t.Context(), repository.Registration{ID: 1001, Username: "viewer", DisplayName: "Viewer", Email: "viewer@example.com"}))
	require.NoError(t, repo.Register(t.Context(), repository.Registration{ID: 1002, Username: "other", DisplayName: "Other", Email: "other@example.com"}))
	var wg sync.WaitGroup
	wg.Go(func() { assert.Error(t, repo.SetLocale(t.Context(), 1001, "invalid-locale")) })
	wg.Go(func() { assert.NoError(t, repo.SetLocale(t.Context(), 1002, "fr")) })
	wg.Go(func() { assert.NoError(t, repo.SetCustomCursor(t.Context(), 1001, false)) })
	wg.Go(func() { assert.Error(t, repo.SetLocale(t.Context(), 9999, "de")) })
	wg.Wait()
	row := client.User.GetX(t.Context(), 1001)
	assert.False(t, row.CustomCursor)
	assert.Equal(t, "fr", client.User.GetX(t.Context(), 1002).Locale)
}

func TestPreferenceRetriesWholeTransaction(t *testing.T) {
	for _, number := range []uint16{1213, 1205} {
		t.Run(map[uint16]string{1213: "deadlock", 1205: "lock wait"}[number], func(t *testing.T) {
			client, repo := durableUsers(t, true)
			for _, id := range []uint64{1001, 1002} {
				require.NoError(t, repo.Register(t.Context(), repository.Registration{ID: id, Username: "viewer", DisplayName: "Viewer", Email: fmt.Sprintf("viewer%d@example.com", id)}))
			}
			var failed atomic.Bool
			client.User.Use(func(next ent.Mutator) ent.Mutator {
				return ent.MutateFunc(func(ctx context.Context, m ent.Mutation) (ent.Value, error) {
					mutation := m.(*ent.UserMutation)
					id, _ := mutation.ID()
					if id == 1002 && failed.CompareAndSwap(false, true) {
						return nil, &mysql.MySQLError{Number: number}
					}
					return next.Mutate(ctx, m)
				})
			})
			var wg sync.WaitGroup
			for _, id := range []uint64{1001, 1002} {
				wg.Go(func() { assert.NoError(t, repo.SetLocale(t.Context(), id, "fr")) })
			}
			wg.Wait()
			for _, id := range []uint64{1001, 1002} {
				row := client.User.GetX(t.Context(), id)
				assert.Equal(t, []any{"fr", int64(2)}, []any{row.Locale, row.StateRevision})
			}
		})
	}
}
