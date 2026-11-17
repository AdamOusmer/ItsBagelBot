// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository_test

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"ItsBagelBot/app/db/commands/ent"
	"ItsBagelBot/app/db/commands/repository"
	"ItsBagelBot/internal/domain/event/data"
	"ItsBagelBot/pkg/bus/bustest"
	"ItsBagelBot/pkg/codec"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
)

func TestCommandCloseWaitsForInFlightUseBeforePublications(t *testing.T) {
	client, _, initial := setup(t)
	require.NoError(t, initial.Close(t.Context()))
	core, logs := observer.New(zap.ErrorLevel)
	pub := bustest.NewPublisher()
	r := repository.NewCommands(client, pub, nil, zap.New(core))
	t.Cleanup(func() { require.NoError(t, r.Close(context.Background())) })
	client.Commands.Create().SetUserID(1001).SetName("hello").SetResponse("hi").SaveX(t.Context())
	entered, release := make(chan struct{}), make(chan struct{})
	client.CommandUseBatch.Use(func(next ent.Mutator) ent.Mutator {
		return ent.MutateFunc(func(ctx context.Context, m ent.Mutation) (ent.Value, error) {
			close(entered)
			<-release
			return next.Mutate(ctx, m)
		})
	})
	used := make(chan error, 1)
	go func() { used <- r.RecordUse(t.Context(), "inflight", data.CommandUsedDTO{UserID: 1001, Name: "hello"}) }()
	<-entered
	require.NoError(t, r.Upsert(1001, spec("hello", "saved", false, 0)))
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
	defer cancel()
	require.ErrorIs(t, r.Close(ctx), context.DeadlineExceeded)
	assert.Equal(t, "saved", client.Commands.Query().OnlyX(t.Context()).Response)
	close(release)
	require.NoError(t, <-used)
	require.NoError(t, r.Close(t.Context()))
	assertPublishedUses(t, pub, 1)
	assert.Zero(t, logs.FilterMessage("dirty keys marked after close").Len())
}

func TestCommandCloseBudgetsEditsAndPublicationDrain(t *testing.T) {
	for _, step := range []string{"edits", "publications"} {
		t.Run(step, func(t *testing.T) {
			client, pub, r := publicationRepository(t)
			var unavailable atomic.Bool
			if step == "edits" {
				unavailable.Store(true)
				client.Commands.Use(unavailableCommandEdits(&unavailable))
				require.NoError(t, r.Upsert(1001, spec("hello", "saved", false, 0)))
			}
			ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
			defer cancel()
			start := time.Now()
			require.ErrorIs(t, r.Close(ctx), context.DeadlineExceeded)
			assert.Less(t, time.Since(start), time.Second)
			unavailable.Store(false)
			pub.release <- struct{}{}
			require.NoError(t, r.Close(t.Context()))
			assert.NotEmpty(t, pub.On(data.SubjectCommandChanged))
		})
	}
}

func unavailableCommandEdits(unavailable *atomic.Bool) ent.Hook {
	return func(next ent.Mutator) ent.Mutator {
		return ent.MutateFunc(func(ctx context.Context, m ent.Mutation) (ent.Value, error) {
			if unavailable.Load() {
				return nil, errors.New("database unavailable")
			}
			return next.Mutate(ctx, m)
		})
	}
}

func TestCommandEditPublishesQueuedStateWhenReloadFails(t *testing.T) {
	client, pub, r := setup(t)
	t.Cleanup(func() { require.NoError(t, r.Close(context.Background())) })
	client.Commands.Intercept(ent.InterceptFunc(func(ent.Querier) ent.Querier {
		return ent.QuerierFunc(func(context.Context, ent.Query) (ent.Value, error) {
			return nil, errors.New("reload unavailable")
		})
	}))
	require.NoError(t, r.Upsert(1001, spec("hello", "queued", false, 0)))
	require.NoError(t, r.Close(t.Context()))
	events := pub.On(data.SubjectCommandChanged)
	require.Len(t, events, 1)
	var dto data.CommandChangedDTO
	require.NoError(t, codec.Unmarshal(events[0].Payload, &dto))
	assert.Equal(t, "queued", dto.Response)
}
