// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package rpc

import (
	"context"
	"strconv"
	"testing"
	"time"

	livekey "ItsBagelBot/internal/domain/live"
	projectorrpc "ItsBagelBot/internal/domain/rpc/projector"
	"ItsBagelBot/internal/projection"
	pkg_valkey "ItsBagelBot/pkg/valkey"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestLiveReadsFollowATwitchRestoredStream(t *testing.T) {
	client := moduleTestValkey(t)
	store := projection.NewStore(client)
	ctx := context.Background()
	id := uint64(time.Now().UnixNano())
	sid := strconv.FormatUint(id, 10)

	_, err := store.SetStreamLive(ctx, id, projection.StreamLive{Version: 2000})
	require.NoError(t, err)
	_, err = pkg_valkey.VersionedSlot{Key: livekey.Key(id), VerKey: livekey.VerKey(id)}.Set(ctx, client, pkg_valkey.SlotWrite{Version: 3000, TTL: time.Minute, VerTTL: 2 * time.Minute})
	require.NoError(t, err)

	live := (&liveRPC{store: store, log: zap.NewNop()}).handleGet(ctx, projectorrpc.LiveRequest{BroadcasterID: sid})
	assert.Equal(t, projectorrpc.LiveReply{BroadcasterID: sid, Live: true, Known: true, Version: 3000}, live)

	info := (&streamInfoRPC{store: store, log: zap.NewNop()}).handleGet(ctx, projectorrpc.StreamInfoRequest{BroadcasterID: sid})
	assert.True(t, info.Live, "the dashboard must show a stream Twitch confirmed live")
}
