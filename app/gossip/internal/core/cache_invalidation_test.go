// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package core_test

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"ItsBagelBot/app/gossip/internal/core"
	"ItsBagelBot/app/gossip/internal/providertest"

	"github.com/stretchr/testify/require"
)

type slowSetStore struct {
	core.Store
	started chan struct{}
	release chan struct{}
}

func (s *slowSetStore) Set(ctx context.Context, key string, value []byte, ttl time.Duration) error {
	if key == "blocked" {
		close(s.started)
		<-s.release
	}
	return s.Store.Set(ctx, key, value, ttl)
}

func TestSlowSetDoesNotBlockOtherKeys(t *testing.T) {
	eachFlavor(t, func(t *testing.T, f cacheFlavor) {
		store := &slowSetStore{Store: providertest.NewMemStore(), started: make(chan struct{}), release: make(chan struct{})}
		c := core.NewCache(store)
		var calls atomic.Int32
		_, err := f.get(c, "hit", nil, fetchOf(&calls, "cached", nil))
		require.NoError(t, err)
		slowDone := make(chan error, 1)
		go func() {
			_, err := f.get(c, "blocked", nil, fetchOf(&calls, "slow", nil))
			slowDone <- err
		}()
		<-store.started
		defer func() {
			close(store.release)
			require.NoError(t, <-slowDone)
		}()
		for _, key := range []string{"hit", "miss"} {
			finished := make(chan error, 1)
			go func() {
				_, err := f.get(c, key, nil, fetchOf(&calls, "new", nil))
				finished <- err
			}()
			select {
			case err := <-finished:
				require.NoError(t, err)
			case <-time.After(time.Second):
				t.Fatal("another key waited for a slow Set")
			}
		}
	})
}

func TestDeletePreventsAnOldFillFromReplacingANewerValue(t *testing.T) {
	eachFlavor(t, func(t *testing.T, f cacheFlavor) {
		c := core.NewCache(providertest.NewMemStore())
		started, release, finished := make(chan struct{}), make(chan struct{}), make(chan error, 1)
		go func() {
			_, err := f.get(c, "k", nil, func(context.Context) (string, error) {
				close(started)
				<-release
				return "old", nil
			})
			finished <- err
		}()
		<-started
		require.NoError(t, c.DelJSON(t.Context(), "k"))
		var calls atomic.Int32
		got, err := f.get(c, "k", nil, fetchOf(&calls, "new", nil))
		require.NoError(t, err)
		require.Equal(t, "new", got)
		close(release)
		require.NoError(t, <-finished)
		got, err = f.get(c, "k", nil, fetchOf(&calls, "unexpected", nil))
		require.NoError(t, err)
		require.Equal(t, "new", got)
		require.EqualValues(t, 1, calls.Load())
	})
}

func TestDeleteOnlyFencesTheDeletedKeysFill(t *testing.T) {
	eachFlavor(t, func(t *testing.T, f cacheFlavor) {
		c := core.NewCache(providertest.NewMemStore())
		var calls atomic.Int32
		got, err := f.get(c, "k", nil, func(ctx context.Context) (string, error) {
			calls.Add(1)
			return "retained", c.DelJSON(ctx, "other")
		})
		require.NoError(t, err)
		require.Equal(t, "retained", got)
		got, err = f.get(c, "k", nil, fetchOf(&calls, "unexpected", nil))
		require.NoError(t, err)
		require.Equal(t, "retained", got)
		require.EqualValues(t, 1, calls.Load())
	})
}

func TestDeleteFencesAFillWaitingForAdmission(t *testing.T) {
	eachFlavor(t, func(t *testing.T, f cacheFlavor) {
		c := core.NewCache(providertest.NewMemStore())
		started, release, finished := make(chan struct{}), make(chan struct{}), make(chan error, 1)
		var calls atomic.Int32
		go func() {
			_, err := f.get(c, "k", func(context.Context) error {
				close(started)
				<-release
				return nil
			}, fetchOf(&calls, "old", nil))
			finished <- err
		}()
		<-started
		require.NoError(t, c.DelJSON(t.Context(), "k"))
		close(release)
		require.NoError(t, <-finished)
		got, err := f.get(c, "k", nil, fetchOf(&calls, "new", nil))
		require.NoError(t, err)
		require.Equal(t, "new", got)
		require.EqualValues(t, 2, calls.Load())
	})
}

func TestDeleteFencesAnOlderStaleRefresh(t *testing.T) {
	eachFlavor(t, func(t *testing.T, f cacheFlavor) {
		store := providertest.NewMemStore()
		c := core.NewCache(store)
		seed(t, store, f.entry("stale", false))
		started, release, finished := make(chan struct{}), make(chan struct{}), make(chan struct{})
		_, err := f.get(c, "k", nil, func(ctx context.Context) (string, error) {
			close(started)
			go func() {
				<-ctx.Done()
				close(finished)
			}()
			<-release
			return "old", nil
		})
		require.NoError(t, err)
		<-started
		require.NoError(t, c.DelJSON(t.Context(), "k"))
		var calls atomic.Int32
		got, err := f.get(c, "k", nil, fetchOf(&calls, "new", nil))
		require.NoError(t, err)
		require.Equal(t, "new", got)
		close(release)
		<-finished
		got, err = f.get(c, "k", nil, fetchOf(&calls, "unexpected", nil))
		require.NoError(t, err)
		require.Equal(t, "new", got)
		require.EqualValues(t, 1, calls.Load())
	})
}
