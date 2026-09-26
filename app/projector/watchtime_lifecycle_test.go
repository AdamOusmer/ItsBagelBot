// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package main

import (
	"context"
	"os"
	"strconv"
	"testing"
	"time"

	"ItsBagelBot/internal/domain/event/data"
	"ItsBagelBot/internal/projection"
	"ItsBagelBot/internal/watchtime"

	"github.com/stretchr/testify/require"
	"github.com/valkey-io/valkey-go"
)

type deletionTrackingLiveStore struct {
	*fakeLiveStore
	deleted int
}

func (s *deletionTrackingLiveStore) DeleteLiveCounters(context.Context, uint64, []projection.CounterName) error {
	s.deleted++
	return nil
}

func TestWatchIgnoredDeletionPreservesLiveCounters(t *testing.T) {
	addr := os.Getenv("VALKEY_TEST_ADDR")
	if addr == "" {
		t.Skip("VALKEY_TEST_ADDR requires isolated real Valkey")
	}
	client, err := valkey.NewClient(valkey.ClientOption{InitAddress: []string{addr}, DisableCache: true})
	require.NoError(t, err)
	defer client.Close()
	ctx := t.Context()
	id := uint64(time.Now().UnixNano()%100000000 + 8600000000)
	sid := strconv.FormatUint(id, 10)
	t.Cleanup(func() {
		client.Do(context.Background(), client.B().Del().Key("settings:"+sid, watchtime.AdmissionKey(id)).Build())
	})
	store := projection.NewStore(client)
	require.NoError(t, store.SetUser(ctx, id, projection.UserProjection{AccountCreatedAt: 101, StateRevision: 1, IsActive: true, Status: "paid"}))
	live := &deletionTrackingLiveStore{fakeLiveStore: newFakeLiveStore()}
	projector := &Projector{store: store, live: live}
	for _, epoch := range []int64{0, 100} {
		require.NoError(t, projector.applyUserDeleted(ctx, data.UserDeletedDTO{UserID: id, AccountCreatedAt: epoch}))
		require.Zero(t, live.deleted, "ignored deletion must not touch the recreated account's live totals")
		_, active, _, _, _, err := store.GetUser(ctx, id)
		require.NoError(t, err)
		require.True(t, active)
	}
}
