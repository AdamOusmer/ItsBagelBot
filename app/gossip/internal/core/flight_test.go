// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package core_test

import (
	"context"
	"testing"
	"time"

	"ItsBagelBot/app/gossip/internal/core"
	"ItsBagelBot/app/gossip/internal/providertest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type contextCacheCall func(context.Context, *core.Cache, admitFunc, fetchFunc) (string, error)

var contextCaches = map[string]contextCacheCall{
	"typed": func(ctx context.Context, c *core.Cache, admit admitFunc, fetch fetchFunc) (string, error) {
		return core.Cached(ctx, c, "shared", time.Minute, time.Minute, admit, fetch)
	},
	"bytes": func(ctx context.Context, c *core.Cache, admit admitFunc, fetch fetchFunc) (string, error) {
		b, err := core.CachedBytes(ctx, c, "shared", admit, func(ctx context.Context) ([]byte, time.Duration, error) {
			value, err := fetch(ctx)
			return []byte(value), time.Minute, err
		})
		return string(b), err
	},
}

func TestCacheWaiterCanCancelWithoutWaitingForTheFill(t *testing.T) {
	for name, call := range contextCaches {
		t.Run(name, func(t *testing.T) {
			cache := core.NewCache(providertest.NewMemStore())
			started, release, filled := make(chan struct{}), make(chan struct{}), make(chan struct{})
			fetch := func(context.Context) (string, error) {
				close(started)
				<-release
				return "value", nil
			}
			go func() {
				defer close(filled)
				_, _ = call(context.Background(), cache, nil, fetch)
			}()
			<-started
			defer func() { close(release); <-filled }()
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
			defer cancel()
			result := make(chan error, 1)
			go func() { _, err := call(ctx, cache, nil, fetch); result <- err }()
			select {
			case err := <-result:
				require.ErrorIs(t, err, context.DeadlineExceeded)
			case <-time.After(time.Second):
				t.Fatal("waiter stayed blocked on the shared fill after its deadline")
			}
		})
	}
}

func TestCacheInitiatorCancellationDoesNotFailOtherWaiters(t *testing.T) {
	for name, call := range contextCaches {
		t.Run(name, func(t *testing.T) {
			cache := core.NewCache(providertest.NewMemStore())
			started, release, finished := make(chan struct{}), make(chan struct{}), make(chan struct{})
			fetch := func(ctx context.Context) (string, error) {
				defer close(finished)
				close(started)
				<-release
				assert.NoError(t, ctx.Err(), "one canceled caller must not cancel the shared fill")
				deadline, ok := ctx.Deadline()
				assert.True(t, ok, "detached work must retain a bounded lifetime")
				assert.LessOrEqual(t, time.Until(deadline), 15*time.Second)
				return "value", nil
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			first := make(chan error, 1)
			go func() { _, err := call(ctx, cache, nil, fetch); first <- err }()
			<-started
			admitted := make(chan struct{})
			second := make(chan string, 1)
			go func() {
				value, err := call(context.Background(), cache, func(context.Context) error { close(admitted); return nil }, fetch)
				assert.NoError(t, err)
				second <- value
			}()
			<-admitted
			cancel()
			select {
			case err := <-first:
				assert.ErrorIs(t, err, context.Canceled)
			case <-time.After(time.Second):
				t.Error("the initiating caller did not cancel promptly")
			}
			close(release)
			<-finished
			require.Equal(t, "value", <-second)
			value, err := call(context.Background(), cache, nil, func(context.Context) (string, error) {
				t.Error("the shared result must warm the cache")
				return "", nil
			})
			require.NoError(t, err)
			assert.Equal(t, "value", value)
		})
	}
}

func TestSharedFetchStillHonorsItsStageDeadline(t *testing.T) {
	for name, call := range contextCaches {
		t.Run(name, func(t *testing.T) {
			ctx, cancel := core.WithFetchTimeout(context.Background(), 30*time.Millisecond)
			defer cancel()
			stopped := make(chan error, 1)
			_, err := call(ctx, core.NewCache(providertest.NewMemStore()), nil, func(work context.Context) (string, error) {
				<-work.Done()
				stopped <- work.Err()
				return "", work.Err()
			})
			require.ErrorIs(t, err, context.DeadlineExceeded)
			select {
			case err := <-stopped:
				assert.ErrorIs(t, err, context.DeadlineExceeded)
			case <-time.After(time.Second):
				t.Fatal("shared work continued past the upstream stage deadline")
			}
		})
	}
}

func TestCacheInitiatorDeadlineDoesNotFailOtherWaiters(t *testing.T) {
	for name, call := range contextCaches {
		t.Run(name, func(t *testing.T) {
			cache := core.NewCache(providertest.NewMemStore())
			started, release := make(chan struct{}), make(chan struct{})
			fetch := func(ctx context.Context) (string, error) {
				close(started)
				<-release
				return "value", ctx.Err()
			}
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
			defer cancel()
			first := make(chan error, 1)
			go func() { _, err := call(ctx, cache, nil, fetch); first <- err }()
			<-started
			admitted := make(chan struct{})
			second := make(chan error, 1)
			go func() {
				_, err := call(context.Background(), cache, func(context.Context) error { close(admitted); return nil }, fetch)
				second <- err
			}()
			<-admitted
			require.ErrorIs(t, <-first, context.DeadlineExceeded)
			close(release)
			require.NoError(t, <-second, "a short waiter deadline must not poison another caller's fill")
		})
	}
}

func TestSharedFetchPanicReturnsAFailureAndAllowsRetry(t *testing.T) {
	for name, call := range contextCaches {
		t.Run(name, func(t *testing.T) {
			cache := core.NewCache(providertest.NewMemStore())
			ctx := core.WithRequestTelemetry(context.Background(), "fortnite", "stats")
			_, err := call(ctx, cache, nil, func(context.Context) (string, error) {
				panic("private upstream response")
			})
			require.EqualError(t, err, "gossip shared fetch panicked")
			assert.Equal(t, "internal_error", core.ReplyOutcome(ctx, []byte(`{"error":"stats lookup failed"}`)))
			value, err := call(context.Background(), cache, nil, func(context.Context) (string, error) { return "recovered", nil })
			require.NoError(t, err)
			assert.Equal(t, "recovered", value)
		})
	}
}
