// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository_test

import (
	"context"
	"errors"
	"fmt"
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
)

type pausedCommandPublisher struct {
	*bustest.Publisher
	calls   atomic.Int64
	expired atomic.Int64
	entered chan struct{}
	release chan struct{}
	fail    func(context.Context) error
}

func (p *pausedCommandPublisher) PublishOwned(ctx context.Context, subject string, payload []byte) error {
	switch p.calls.Add(1) {
	case 1:
		close(p.entered)
		<-p.release
	case 2:
		if p.fail != nil {
			return p.fail(ctx)
		}
	}
	if err := ctx.Err(); err != nil {
		p.expired.Add(1)
		return err
	}
	return p.Publisher.PublishOwned(ctx, subject, payload)
}

func publicationRepository(t *testing.T) (*ent.Client, *pausedCommandPublisher, *repository.Commands) {
	t.Helper()
	client, _, initial := setup(t)
	require.NoError(t, initial.Close(context.Background()))
	pub := &pausedCommandPublisher{Publisher: bustest.NewPublisher(), entered: make(chan struct{}), release: make(chan struct{})}
	r := repository.NewCommands(client, pub, nil, zap.NewNop())
	t.Cleanup(func() { require.NoError(t, r.Close(context.Background())) })
	t.Cleanup(func() { close(pub.release) })
	client.Commands.Create().SetUserID(1001).SetName("hello").SetResponse("old").SaveX(t.Context())
	require.NoError(t, r.RecordUse(t.Context(), "first", data.CommandUsedDTO{UserID: 1001, Name: "hello", Count: 1}))
	<-pub.entered
	return client, pub, r
}

func TestCommandUsePublicationCoalescesWithoutBlockingEdits(t *testing.T) {
	client, pub, r := publicationRepository(t)
	f := useGroupFixture{repos: []*repository.Commands{r}}
	requests := make([]useRequest, 128)
	for i := range requests {
		requests[i] = useRequest{id: fmt.Sprint("queued-", i), name: "hello", count: 1}
	}
	f.submit(t, requests)
	assert.Equal(t, int64(129), client.Commands.Query().OnlyX(t.Context()).Uses)
	require.NoError(t, r.Upsert(1001, spec("hello", "edited", false, 0)))
	closed := make(chan error, 1)
	go func() { closed <- r.Close(context.Background()) }()
	require.Eventually(t, func() bool { return len(pub.On(data.SubjectCommandChanged)) == 1 }, time.Second, time.Millisecond)
	var edit data.CommandChangedDTO
	require.NoError(t, codec.Unmarshal(pub.On(data.SubjectCommandChanged)[0].Payload, &edit))
	assert.Equal(t, "edited", edit.Response)
	pub.release <- struct{}{}
	require.NoError(t, <-closed)
	assert.LessOrEqual(t, len(pub.On(data.SubjectCommandChanged)), 3)
}

func TestCommandUsePublicationRetriesChunksDuringClose(t *testing.T) {
	for _, failure := range []string{"publish", "timeout", "reload"} {
		t.Run(failure, func(t *testing.T) {
			client, pub, r := publicationRepository(t)
			configurePublicationFailure(client, pub, failure)
			queuePublicationUses(t, client, r)
			closed := make(chan error, 1)
			go func() { closed <- r.Close(context.Background()) }()
			pub.release <- struct{}{}
			require.NoError(t, <-closed)
			assertPublishedUses(t, pub.Publisher, 131)
			assert.Zero(t, pub.expired.Load(), "later chunks must receive fresh deadlines")
		})
	}
}

func queuePublicationUses(t *testing.T, client *ent.Client, r *repository.Commands) {
	t.Helper()
	f := useGroupFixture{repos: []*repository.Commands{r}}
	requests := make([]useRequest, 130)
	for i := range requests {
		name := fmt.Sprint("key", i)
		client.Commands.Create().SetUserID(1001).SetName(name).SetResponse("hi").SaveX(t.Context())
		requests[i] = useRequest{id: name, name: name, count: 1}
	}
	f.submit(t, requests)
}

func TestCommandUsePublicationStopsCycleAtFirstFailure(t *testing.T) {
	for _, failure := range []string{"publish", "reload"} {
		t.Run(failure, func(t *testing.T) {
			client, pub, r := publicationRepository(t)
			queuePublicationUses(t, client, r)
			var reloads atomic.Int64
			client.Commands.Intercept(ent.InterceptFunc(func(next ent.Querier) ent.Querier {
				return ent.QuerierFunc(func(ctx context.Context, q ent.Query) (ent.Value, error) {
					if reloads.Add(1) == 1 && failure == "reload" {
						return nil, errors.New("reload unavailable")
					}
					return next.Query(ctx, q)
				})
			}))
			if failure == "publish" {
				configurePublicationFailure(client, pub, failure)
			}
			ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
			defer cancel()
			pub.release <- struct{}{}
			require.ErrorIs(t, r.Close(ctx), context.DeadlineExceeded)
			assert.Equal(t, int64(1), reloads.Load(), "later chunks must wait for the retry cycle")
			require.NoError(t, r.Close(t.Context()))
			assertPublishedUses(t, pub.Publisher, 131)
		})
	}
}

func TestCommandUseInvalidatesCacheBeforeFailedReload(t *testing.T) {
	client, pub, r := publicationRepository(t)
	client.Commands.Create().SetUserID(1001).SetName("next").SetResponse("hi").SaveX(t.Context())
	_, err := r.List(t.Context(), 1001)
	require.NoError(t, err)
	require.NoError(t, r.RecordUse(t.Context(), "next", data.CommandUsedDTO{UserID: 1001, Name: "next"}))
	entered, release := make(chan struct{}), make(chan struct{})
	t.Cleanup(func() { close(release) })
	var failed atomic.Bool
	client.Commands.Intercept(ent.InterceptFunc(func(next ent.Querier) ent.Querier {
		return ent.QuerierFunc(func(ctx context.Context, q ent.Query) (ent.Value, error) {
			if failed.CompareAndSwap(false, true) {
				close(entered)
				<-release
				return nil, errors.New("reload unavailable")
			}
			return next.Query(ctx, q)
		})
	}))
	pub.release <- struct{}{}
	<-entered
	views, err := r.List(t.Context(), 1001)
	require.NoError(t, err)
	for _, view := range views {
		assert.Equal(t, int64(1), view.Uses, view.Name)
	}
	release <- struct{}{}
	require.NoError(t, r.Close(t.Context()))
	assertPublishedUses(t, pub.Publisher, 2)
}

func configurePublicationFailure(client *ent.Client, pub *pausedCommandPublisher, failure string) {
	switch failure {
	case "publish":
		pub.fail = func(context.Context) error { return errors.New("publisher unavailable") }
	case "timeout":
		pub.fail = func(ctx context.Context) error { <-ctx.Done(); return ctx.Err() }
	case "reload":
		var failed atomic.Bool
		client.Commands.Intercept(ent.InterceptFunc(func(next ent.Querier) ent.Querier {
			return ent.QuerierFunc(func(ctx context.Context, q ent.Query) (ent.Value, error) {
				if failed.CompareAndSwap(false, true) {
					return nil, errors.New("reload unavailable")
				}
				return next.Query(ctx, q)
			})
		}))
	}
}

func assertPublishedUses(t *testing.T, pub *bustest.Publisher, want int) {
	t.Helper()
	names := make(map[string]int64)
	for _, event := range pub.On(data.SubjectCommandChanged) {
		var dto data.CommandChangedDTO
		require.NoError(t, codec.Unmarshal(event.Payload, &dto))
		names[dto.Name] = dto.Uses
	}
	assert.Len(t, names, want)
	for name, uses := range names {
		assert.Equal(t, int64(1), uses, name)
	}
}
