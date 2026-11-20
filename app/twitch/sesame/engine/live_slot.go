// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"context"
	"strconv"
	"time"

	"ItsBagelBot/app/twitch/sesame/engine/draw"
	livekey "ItsBagelBot/internal/domain/live"
	pkg_valkey "ItsBagelBot/pkg/valkey"

	"github.com/valkey-io/valkey-go"
)

func liveSlot(broadcasterID uint64) pkg_valkey.VersionedSlot {
	return pkg_valkey.VersionedSlot{Key: liveKey(broadcasterID), VerKey: livekey.VerKey(broadcasterID)}
}

func clearLiveKey(ctx context.Context, client valkey.Client, broadcasterID uint64, version int64) (bool, error) {
	return liveSlot(broadcasterID).Clear(ctx, client, version, livekey.VerTTL)
}

func (s *ValkeyLiveStore) DrawLiveState(ctx context.Context, channel uint64) (draw.LiveState, error) {
	on, err := s.IsLive(ctx, channel)
	if err != nil {
		return draw.LiveState{}, err
	}
	raw, _, err := pkg_valkey.ReadString(ctx, s.client, livekey.VerKey(channel))
	ms, _ := strconv.ParseInt(raw, 10, 64)
	state := draw.LiveState{Live: on}
	if ms > 0 {
		state.ChangedAt = time.UnixMilli(ms)
	}
	return state, err
}
