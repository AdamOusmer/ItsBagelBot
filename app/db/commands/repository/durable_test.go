// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"ItsBagelBot/app/db/commands/ent"
	"ItsBagelBot/app/db/commands/ent/commands"
	"ItsBagelBot/app/db/commands/repository"
	"ItsBagelBot/internal/testdb"
	"ItsBagelBot/pkg/bus/bustest"
	entsql "entgo.io/ent/dialect/sql"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func durableCommands(t *testing.T, mysql bool) (*ent.Client, *repository.Commands) {
	t.Helper()
	if !mysql {
		client, _, repo := setup(t)
		t.Cleanup(func() { require.NoError(t, repo.Close(context.Background())) })
		return client, repo
	}
	raw := testdb.MySQL(t, "commands_durable_")
	client := ent.NewClient(ent.Driver(entsql.OpenDB("mysql", raw)))
	require.NoError(t, client.Schema.Create(t.Context()))
	repo := repository.NewCommands(client, bustest.NewPublisher(), nil, zap.NewNop())
	t.Cleanup(func() { require.NoError(t, repo.Close(context.Background())) })
	return client, repo
}

func TestDashboardCommandDurability(t *testing.T) {
	for _, mysql := range []bool{false, true} {
		t.Run(map[bool]string{false: "SQLite", true: "MySQL"}[mysql], func(t *testing.T) {
			for _, test := range []struct {
				name string
				run  func(*testing.T, bool)
			}{
				{"visible", commandVisible}, {"same key", commandSameKey}, {"poison", commandPoison},
				{"delete pending", commandDeletePending}, {"rename pending", commandRenamePending},
			} {
				t.Run(test.name, func(t *testing.T) { test.run(t, mysql) })
			}
		})
	}
}

func commandVisible(t *testing.T, mysql bool) {
	client, repo := durableCommands(t, mysql)
	_, err := repo.List(t.Context(), 1001)
	require.NoError(t, err)
	require.NoError(t, repo.Upsert(1001, spec("hello", "committed", false, 0)))
	assert.Equal(t, "committed", client.Commands.Query().OnlyX(t.Context()).Response)
	views, err := repo.List(t.Context(), 1001)
	require.NoError(t, err)
	require.Len(t, views, 1)
	assert.Equal(t, "committed", views[0].Response)
}

func commandSameKey(t *testing.T, mysql bool) {
	client, repo := durableCommands(t, mysql)
	var wg sync.WaitGroup
	for range 48 {
		wg.Go(func() { assert.NoError(t, repo.Upsert(1001, spec("hello", "shared", false, 0))) })
	}
	wg.Wait()
	assert.Equal(t, "shared", client.Commands.Query().OnlyX(t.Context()).Response)
}

func commandPoison(t *testing.T, mysql bool) {
	client, repo := durableCommands(t, mysql)
	client.Commands.Use(func(next ent.Mutator) ent.Mutator {
		return ent.MutateFunc(func(ctx context.Context, m ent.Mutation) (ent.Value, error) {
			if name, _ := m.Field("name"); name == "bad" {
				_ = m.SetField("response", "")
			}
			return next.Mutate(ctx, m)
		})
	})
	var wg sync.WaitGroup
	for _, name := range []string{"good", "bad", "also-good"} {
		wg.Go(func() { assert.Equal(t, name == "bad", repo.Upsert(1001, spec(name, "saved", false, 0)) != nil) })
	}
	wg.Wait()
	assert.Equal(t, 2, client.Commands.Query().CountX(t.Context()))
}

func commandDeletePending(t *testing.T, mysql bool) { commandPendingOrder(t, mysql, false) }
func commandRenamePending(t *testing.T, mysql bool) { commandPendingOrder(t, mysql, true) }

func commandPendingOrder(t *testing.T, mysql, rename bool) {
	client, repo := durableCommands(t, mysql)
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	client.Commands.Use(func(next ent.Mutator) ent.Mutator {
		return ent.MutateFunc(func(ctx context.Context, m ent.Mutation) (ent.Value, error) {
			once.Do(func() { close(entered); <-release })
			return next.Mutate(ctx, m)
		})
	})
	edited, changed := make(chan error, 1), make(chan error, 1)
	go func() { edited <- repo.Upsert(1001, spec("old", "saved", false, 0)) }()
	<-entered
	go func() {
		if rename {
			changed <- repo.Rename(t.Context(), 1001, "old", spec("new", "renamed", false, 0))
			return
		}
		changed <- repo.Delete(t.Context(), 1001, "old")
	}()
	select {
	case err := <-changed:
		t.Errorf("row changed before admitted edit committed: %v", err)
		changed <- err
	case <-time.After(10 * time.Millisecond):
	}
	close(release)
	require.NoError(t, <-edited)
	require.NoError(t, <-changed)
	require.NoError(t, repo.Close(t.Context()))
	assert.Zero(t, client.Commands.Query().Where(commands.NameEQ("old")).CountX(t.Context()))
	assert.Equal(t, rename, client.Commands.Query().Where(commands.NameEQ("new")).ExistX(t.Context()))
}
