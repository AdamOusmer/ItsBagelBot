// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository_test

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"ItsBagelBot/app/db/commands/ent"
	"ItsBagelBot/app/db/commands/repository"
	"ItsBagelBot/internal/domain/event/data"
	"ItsBagelBot/pkg/bus/bustest"
	"ItsBagelBot/pkg/codec"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestCommandOrderingIsScopedToBroadcaster(t *testing.T) {
	for _, mysql := range []bool{false, true} {
		t.Run(map[bool]string{false: "SQLite", true: "MySQL"}[mysql], func(t *testing.T) {
			client, repo := durableCommands(t, mysql)
			entered, release := make(chan struct{}), make(chan struct{})
			client.Commands.Use(func(next ent.Mutator) ent.Mutator {
				return ent.MutateFunc(func(ctx context.Context, m ent.Mutation) (ent.Value, error) {
					if m.Op().Is(ent.OpDelete) {
						close(entered)
						<-release
					}
					return next.Mutate(ctx, m)
				})
			})
			deleted := make(chan error, 1)
			go func() { deleted <- repo.Delete(t.Context(), 1001, "old") }()
			<-entered
			ctx, cancel := context.WithCancel(t.Context())
			cancel()
			require.ErrorIs(t, repo.UpsertContext(ctx, 1001, spec("hello", "canceled", false, 0)), context.Canceled)
			require.ErrorIs(t, repo.Rename(ctx, 1001, "old", spec("new", "canceled", false, 0)), context.Canceled)
			waitCtx, stop := context.WithTimeout(t.Context(), 20*time.Millisecond)
			defer stop()
			waitErr := repo.DeleteAllForUser(waitCtx, 1001, time.Now())
			saveErr := repo.Upsert(1002, spec("hello", "independent", false, 0))
			close(release)
			require.ErrorIs(t, waitErr, context.DeadlineExceeded)
			require.NoError(t, saveErr)
			require.NoError(t, <-deleted)
		})
	}
}

type deletionPublisher struct {
	*bustest.Publisher
	entered, release chan struct{}
}

func (p *deletionPublisher) PublishOwned(ctx context.Context, topic string, payload []byte) error {
	var event data.CommandChangedDTO
	if err := codec.Unmarshal(payload, &event); err != nil {
		return err
	}
	if event.Deleted {
		close(p.entered)
		select {
		case <-p.release:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return p.Publisher.PublishOwned(ctx, topic, payload)
}

func TestExclusiveCommandOrdering(t *testing.T) {
	for _, mysql := range []bool{false, true} {
		t.Run(map[bool]string{false: "SQLite", true: "MySQL"}[mysql], func(t *testing.T) {
			for _, rename := range []bool{false, true} {
				t.Run(map[bool]string{false: "delete", true: "rename"}[rename], func(t *testing.T) {
					commandPublicationOrdering(t, mysql, rename)
					commandPanicUnlock(t, mysql, rename)
				})
			}
		})
	}
}

func changeCommand(ctx context.Context, repo *repository.Commands, rename bool) error {
	if rename {
		return repo.Rename(ctx, 1001, "old", spec("new", "renamed", false, 0))
	}
	return repo.Delete(ctx, 1001, "old")
}

func commandPublicationOrdering(t *testing.T, mysql, rename bool) {
	t.Helper()
	client, _ := durableCommands(t, mysql)
	pub := &deletionPublisher{Publisher: bustest.NewPublisher(), entered: make(chan struct{}), release: make(chan struct{})}
	repo := repository.NewCommands(client, pub, nil, zap.NewNop())
	t.Cleanup(func() { require.NoError(t, repo.Close(context.Background())) })
	require.NoError(t, repo.Upsert(1001, spec("old", "original", false, 0)))
	changed := make(chan error, 1)
	go func() { changed <- changeCommand(t.Context(), repo, rename) }()
	<-pub.entered
	waitCtx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
	defer cancel()
	saveErr := repo.UpsertContext(waitCtx, 1001, spec("old", "recreated", false, 0))
	close(pub.release)
	require.ErrorIs(t, saveErr, context.DeadlineExceeded)
	require.NoError(t, <-changed)
	require.NoError(t, repo.Upsert(1001, spec("old", "recreated", false, 0)))
	var latest data.CommandChangedDTO
	for _, message := range pub.On(data.SubjectCommandChanged) {
		var event data.CommandChangedDTO
		require.NoError(t, codec.Unmarshal(message.Payload, &event))
		if event.Name == "old" {
			latest = event
		}
	}
	require.Equal(t, []any{false, "recreated"}, []any{latest.Deleted, latest.Response})
}

func commandPanicUnlock(t *testing.T, mysql, rename bool) {
	t.Helper()
	client, repo := durableCommands(t, mysql)
	require.NoError(t, repo.Upsert(1001, spec("old", "original", false, 0)))
	var panicked atomic.Bool
	client.Commands.Use(func(next ent.Mutator) ent.Mutator {
		return ent.MutateFunc(func(ctx context.Context, m ent.Mutation) (ent.Value, error) {
			if panicked.CompareAndSwap(false, true) {
				panic("command mutation")
			}
			return next.Mutate(ctx, m)
		})
	})
	require.Panics(t, func() { _ = changeCommand(t.Context(), repo, rename) })
	require.NoError(t, repo.Upsert(1001, spec("old", "recovered", false, 0)))
}
