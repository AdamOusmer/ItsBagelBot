// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package main

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"ItsBagelBot/internal/projection"
	"ItsBagelBot/pkg/bus"

	"github.com/stretchr/testify/require"
	"github.com/valkey-io/valkey-go"
	"go.uber.org/zap"
)

func TestStaleStreamOfflineLeavesANewerLiveProjection(t *testing.T) {
	addr := os.Getenv("VALKEY_TEST_ADDR")
	if addr == "" {
		t.Skip("VALKEY_TEST_ADDR requires isolated real Valkey")
	}
	client, err := valkey.NewClient(valkey.ClientOption{InitAddress: []string{addr}, DisableCache: true})
	require.NoError(t, err)
	defer client.Close()
	ctx := t.Context()
	id := uint64(time.Now().UnixNano()%100000000 + 8700000000)
	t.Cleanup(func() {
		client.Do(context.Background(), client.B().Del().Key(fmt.Sprintf("settings:%d", id)).Build())
	})
	store := projection.NewStore(client)
	online := time.Date(2026, 10, 1, 17, 16, 7, 0, time.UTC)
	_, err = store.SetStreamLive(ctx, id, projection.StreamLive{Live: true, Version: online.UnixMilli()})
	require.NoError(t, err)

	p := &Projector{store: store, log: zap.NewNop()}
	staleOffline := fmt.Sprintf(`{"type":"stream.offline","event":{"broadcaster_user_id":"%d"},"received_at":%q}`,
		id, online.Add(-time.Second).Format(time.RFC3339Nano))
	require.NoError(t, p.HandleStreamEvent(&bus.Message{UUID: "stale-offline", Payload: []byte(staleOffline)}))

	got, err := store.GetStreamLive(ctx, id)
	require.NoError(t, err)
	require.Equal(t, projection.StreamLive{Live: true, Known: true, Version: online.UnixMilli()}, got)
}
