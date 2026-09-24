// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package spotify

import (
	"context"

	"ItsBagelBot/pkg/monitor"

	"go.uber.org/zap"
)

type keyRotator interface {
	Rotate(ctx context.Context, broadcasterID, prevToken, newToken string) error
}

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
