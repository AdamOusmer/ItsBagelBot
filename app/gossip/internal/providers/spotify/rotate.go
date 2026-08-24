// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package spotify

import (
	"context"

	"ItsBagelBot/pkg/monitor"

	"go.uber.org/zap"
)

// keyRotator is the optional write-back half of the key resolver: custody
// accepts a compare-and-swap replacement when Spotify rotates a refresh token
// on exchange. Declared here rather than on provider.Deps so the shared
// BroadcasterKeyResolver contract — which govee also implements — stays
// read-only; core.SpotifyKeyClient satisfies it, test fakes need not.
type keyRotator interface {
	Rotate(ctx context.Context, broadcasterID, prevToken, newToken string) error
}

// persistRotation writes a rotated refresh token back to custody. Best-effort
// by design: the freshly minted access token is already valid, so a write-back
// failure must never fail the mint — it degrades to the pre-write-back
// behavior (loud warn, store keeps the previous token, which Spotify keeps
// valid unless it explicitly invalidates it). Token values never reach a log
// on either path.
func (p *api) persistRotation(ctx context.Context, broadcaster, prev, next string) {
	if next == "" || next == prev {
		return
	}
	log := monitor.TxnLogger(ctx, p.log)
	r, ok := p.keys.(keyRotator)
	if !ok {
		log.Warn("spotify rotated a broadcaster's refresh token; resolver cannot write it back, the modules store still holds the previous one",
			zap.String("broadcaster", broadcaster))
		return
	}
	if err := r.Rotate(ctx, broadcaster, prev, next); err != nil {
		log.Warn("spotify rotated a broadcaster's refresh token; custody write-back failed, the modules store still holds the previous one",
			zap.String("broadcaster", broadcaster), zap.Error(err))
		return
	}
	log.Info("spotify refresh-token rotation persisted to custody",
		zap.String("broadcaster", broadcaster))
}
