// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package hydration_test

import (
	"context"
	"os"
	"strconv"
	"sync"
	"testing"
	"time"

	"ItsBagelBot/app/projector/hydration"
	"ItsBagelBot/internal/domain/event/data"
	livekey "ItsBagelBot/internal/domain/live"
	rpcprojection "ItsBagelBot/internal/domain/rpc/projection"
	"ItsBagelBot/internal/projection"
	"ItsBagelBot/internal/watchtime"

	"github.com/stretchr/testify/require"
	"github.com/valkey-io/valkey-go"
	"go.uber.org/zap"
)

type notifyingStore struct {
	*projection.Store
	written chan string
}

func (s notifyingStore) SetUserWithTTL(ctx context.Context, id uint64, u projection.UserProjection, ttl time.Duration) error {
	defer func() { s.written <- "user" }()
	return s.Store.SetUserWithTTL(ctx, id, u, ttl)
}

func (s notifyingStore) SetModulesWithTTL(ctx context.Context, id uint64, modules []projection.ModuleView, ttl time.Duration) error {
	defer func() { s.written <- "modules" }()
	return s.Store.SetModulesWithTTL(ctx, id, modules, ttl)
}

func (s notifyingStore) SetCommandsWithTTL(ctx context.Context, id uint64, commands []projection.CommandView, ttl time.Duration) error {
	defer func() { s.written <- "commands" }()
	return s.Store.SetCommandsWithTTL(ctx, id, commands, ttl)
}

func (s notifyingStore) awaitFill(t *testing.T) {
	t.Helper()
	for range 3 {
		select {
		case <-s.written:
		case <-time.After(5 * time.Second):
			t.Fatal("hydration did not finish writing every section")
		}
	}
}

func hydrationWatchStore(t *testing.T) (valkey.Client, notifyingStore, *watchtime.Store, uint64) {
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
	store := notifyingStore{Store: projection.NewStore(client), written: make(chan string, 16)}
	return client, store, watchtime.NewStore(client), id
}

func watchReplies(onSecondUser func()) replies {
	return replies{
		"users": func(attempt int) any {
			if attempt == 2 {
				onSecondUser()
			}
			return rpcprojection.UserReply{AccountCreatedAt: 100, StateRevision: 1, Status: "paid", IsActive: true}
		},
		"modules": func(int) any {
			return rpcprojection.ModulesReply{Modules: []projection.ModuleView{{
				Name: "loyalty", IsEnabled: true, Revision: 1, AccountCreatedAt: 100,
			}}}
		},
	}
}

func TestHydrationCannotAdmitOfflineWatchtime(t *testing.T) {
	for _, projectedLive := range []bool{false, true} {
		t.Run(strconv.FormatBool(projectedLive), func(t *testing.T) {
			client, store, awards, id := hydrationWatchStore(t)
			ctx := t.Context()
			nc, up := newUpstream(t, watchReplies(func() {}))
			h := hydration.New(store, nc, up.subjects, queryTTL, liveTTL, 1, zap.NewNop())
			// Even a forced go-live settings refresh cannot establish live status.
			h.RefreshAsync(id)
			store.awaitFill(t)
			require.NoError(t, store.SetStreamLive(ctx, id, projectedLive))
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
	secondUser, secondUserStarted := signal()
	release := make(chan struct{})
	var releaseOnce sync.Once
	releaseFill := func() { releaseOnce.Do(func() { close(release) }) }
	defer releaseFill()
	nc, up := newUpstream(t, watchReplies(func() {
		secondUser()
		select {
		case <-release:
		case <-time.After(5 * time.Second):
		}
	}))
	h := hydration.New(store, nc, up.subjects, queryTTL, liveTTL, 1, zap.NewNop())
	h.RefreshAsync(id)
	store.awaitFill(t)
	require.NoError(t, client.Do(ctx, client.B().Eval().Script(livekey.SetScript).Numkeys(2).
		Key(livekey.Key(id), livekey.VerKey(id)).Arg("2000", "3600", "7200").Build()).Error())
	snapshot, allowed, err := awards.Capture(ctx, id)
	require.NoError(t, err)
	require.True(t, allowed)

	h.RefreshAsync(id)
	waitFor(t, secondUserStarted, "hydration to reach the delayed account fetch")
	require.NoError(t, client.Do(ctx, client.B().Eval().Script(livekey.ClearScript).Numkeys(2).
		Key(livekey.Key(id), livekey.VerKey(id)).Arg("3000", "7200").Build()).Error())
	require.NoError(t, store.SetStreamLive(ctx, id, false))
	releaseFill()
	store.awaitFill(t)
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
