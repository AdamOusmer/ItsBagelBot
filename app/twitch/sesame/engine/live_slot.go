// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"context"

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
