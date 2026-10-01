// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package hydration

import (
	"context"
	"os"
	"strconv"
	"testing"
	"time"

	"ItsBagelBot/internal/domain/event/data"
	livekey "ItsBagelBot/internal/domain/live"
	rpcprojection "ItsBagelBot/internal/domain/rpc/projection"
	"ItsBagelBot/internal/projection"
	"ItsBagelBot/internal/watchtime"

	"github.com/stretchr/testify/require"
	"github.com/valkey-io/valkey-go"
	"go.uber.org/zap"
)

func hydrationWatchStore(t *testing.T) (valkey.Client, *projection.Store, *watchtime.Store, uint64) {
	t.Helper()
	address := os.Getenv("VALKEY_TEST_ADDR")
	if address == "" {
		t.Skip("VALKEY_TEST_ADDR requires an isolated real Valkey")
	}
	client, err := valkey.NewClient(valkey.ClientOption{
		InitAddress: []string{address}, Password: os.Getenv("VALKEY_TEST_PASSWORD"), DisableCache: true,
	})
	require.NoError(t, err)
	t.Cleanup(client.Close)
	id := uint64(time.Now().UnixNano())
	sid := strconv.FormatUint(id, 10)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		client.Do(ctx, client.B().Del().Key("settings:"+sid, livekey.Key(id), livekey.VerKey(id), watchtime.AdmissionKey(id), "loyaltick:state:"+sid, "loyaltick:claim:"+sid).Build())
	})
	return client, projection.NewStore(client), watchtime.NewStore(client), id
}

func watchHydrationFetchers() fetchers {
	fetch := noOpFetchers()
	fetch.user = func(context.Context, uint64) (rpcprojection.UserReply, error) {
		return rpcprojection.UserReply{AccountCreatedAt: 100, StateRevision: 1, Status: "paid", IsActive: true}, nil
	}
	fetch.modules = func(context.Context, uint64) (rpcprojection.ModulesReply, error) {
		return rpcprojection.ModulesReply{Modules: []projection.ModuleView{{
			Name: "loyalty", IsEnabled: true, Revision: 1, AccountCreatedAt: 100,
		}}}, nil
	}
	return fetch
}

func TestHydrationCannotAdmitOfflineWatchtime(t *testing.T) {
	for _, projectedLive := range []bool{false, true} {
		t.Run(strconv.FormatBool(projectedLive), func(t *testing.T) {
			client, store, awards, id := hydrationWatchStore(t)
			ctx := t.Context()
			h := newHydrator(store, watchHydrationFetchers(), 2*time.Hour, 24*time.Hour, 1, zap.NewNop())
			// Even a forced go-live settings refresh cannot establish live status.
			h.run(job{userID: id, force: true, ttl: h.liveTTL})
			_, err := store.SetStreamLive(ctx, id, projection.StreamLive{Live: projectedLive, Version: 1000})
			require.NoError(t, err)
			active, err := client.Do(ctx, client.B().Hget().Key("settings:"+strconv.FormatUint(id, 10)).Field("active").Build()).ToString()
			require.NoError(t, err)
			require.Equal(t, "1", active, "the account really was hydrated")
			_, allowed, err := awards.Capture(ctx, id)
			require.NoError(t, err)
			require.False(t, allowed, "settings hydration or projected live fields cannot replace confirmed live status")
			exists, err := client.Do(ctx, client.B().Exists().Key(livekey.Key(id), "loyaltick:state:"+strconv.FormatUint(id, 10)).Build()).AsInt64()
			require.NoError(t, err)
			require.Zero(t, exists)
		})
	}
}

func TestHydrationFinishingAfterOfflineCannotReviveWatchtime(t *testing.T) {
	client, store, awards, id := hydrationWatchStore(t)
	ctx := t.Context()
	fetch := watchHydrationFetchers()
	h := newHydrator(store, fetch, 2*time.Hour, 24*time.Hour, 1, zap.NewNop())
	h.run(job{userID: id, force: true, ttl: h.liveTTL})
	require.NoError(t, client.Do(ctx, client.B().Eval().Script(livekey.SetScript).Numkeys(2).
		Key(livekey.Key(id), livekey.VerKey(id)).Arg("2000", "3600", "7200").Build()).Error())
	snapshot, allowed, err := awards.Capture(ctx, id)
	require.NoError(t, err)
	require.True(t, allowed)

	started := make(chan struct{})
	release := make(chan struct{})
	done := make(chan struct{})
	defer func() {
		select {
		case <-release:
		default:
			close(release)
		}
		// Join the background fill even when a require or timeout ends the test,
		// before the fixture deletes keys and closes its client.
		if !receiveHydrationSignal(done, operationTimeout+time.Second) {
			t.Error("background hydration did not stop before fixture cleanup")
		}
	}()
	h.fetch.user = func(ctx context.Context, id uint64) (rpcprojection.UserReply, error) {
		close(started)
		select {
		case <-release:
			return fetch.user(ctx, id)
		case <-ctx.Done():
			return rpcprojection.UserReply{}, ctx.Err()
		}
	}
	go func() {
		h.run(job{userID: id, force: true, ttl: h.liveTTL})
		close(done)
	}()
	require.True(t, receiveHydrationSignal(started, time.Second), "hydration did not reach delayed account fetch")
	require.NoError(t, client.Do(ctx, client.B().Eval().Script(livekey.ClearScript).Numkeys(2).
		Key(livekey.Key(id), livekey.VerKey(id)).Arg("3000", "7200").Build()).Error())
	_, err = store.SetStreamLive(ctx, id, projection.StreamLive{Version: 3000})
	require.NoError(t, err)
	close(release)
	require.True(t, receiveHydrationSignal(done, time.Second), "hydration did not finish")
	_, allowed, err = awards.Capture(ctx, id)
	require.NoError(t, err)
	require.False(t, allowed, "a late account/module fill must leave watchtime offline")
	accepted, err := awards.Enqueue(ctx, data.WatchAwardDTO{
		UserID: id, AccountCreatedAt: snapshot.AccountCreatedAt, Generation: snapshot.Generation,
		LiveSession: snapshot.LiveSession, WindowID: "hydration-offline",
		Entries: []data.LoyaltyEarnEntry{{ViewerID: 77, Points: 7, WatchSeconds: 300}},
	})
	require.NoError(t, err)
	require.False(t, accepted, "an award captured while live cannot be accepted after offline hydration")
}

func receiveHydrationSignal(signal <-chan struct{}, timeout time.Duration) bool {
	select {
	case <-signal:
		return true
	case <-time.After(timeout):
		return false
	}
}
