// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package gateway

import (
	"context"
	"os"
	"testing"

	ddiscord "ItsBagelBot/internal/domain/discord"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/valkey-io/valkey-go"
)

func TestCheckpointStoreRoundTripsTheResumePosition(t *testing.T) {
	addr := os.Getenv("VALKEY_TEST_ADDR")
	if addr == "" {
		t.Skip("VALKEY_TEST_ADDR is not set")
	}
	client, err := valkey.NewClient(valkey.ClientOption{
		InitAddress: []string{addr},
		Password:    os.Getenv("VALKEY_TEST_PASSWORD"),
	})
	require.NoError(t, err)
	t.Cleanup(client.Close)
	ctx := context.Background()
	require.NoError(t, client.Do(ctx, client.B().Del().Key(ddiscord.BotCheckpointKey).Build()).Error())
	store := NewCheckpointStore(client)

	_, ok, err := store.Load(ctx)
	require.NoError(t, err)
	assert.False(t, ok, "no checkpoint before the first save")

	want := Resume{SessionID: "sess-1", ResumeURL: "wss://resume", Seq: 42}
	require.NoError(t, store.Save(ctx, want))
	got, ok, err := store.Load(ctx)

	require.NoError(t, err)
	assert.True(t, ok)
	assert.Equal(t, want, got)
	t.Cleanup(func() { _ = client.Do(ctx, client.B().Del().Key(ddiscord.BotCheckpointKey).Build()).Error() })
}
