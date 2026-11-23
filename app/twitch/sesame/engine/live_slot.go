// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"context"
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
	slot := liveSlot(channel)
	client := pkg_valkey.Primary(s.client)
	state, err := readDrawLiveState(ctx, client, slot)
	if err != nil || knownDrawLiveState(state) {
		return state, err
	}
	if _, err := s.IsLive(ctx, channel); err != nil {
		return draw.LiveState{}, err
	}
	return readDrawLiveState(ctx, client, slot)
}

func knownDrawLiveState(state draw.LiveState) bool {
	return state.Live || !state.ChangedAt.IsZero()
}

func readDrawLiveState(ctx context.Context, client valkey.Client, slot pkg_valkey.VersionedSlot) (draw.LiveState, error) {
	value, ms, err := slot.Read(ctx, client)
	state := draw.LiveState{Live: value != ""}
	if ms > 0 {
		state.ChangedAt = time.UnixMilli(ms)
	}
	return state, err
}
