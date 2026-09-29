// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package spotify

import (
	"context"
	"errors"
	"net/http"

	"ItsBagelBot/app/gossip/internal/core"

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

type keyDeadMarker interface {
	MarkDead(ctx context.Context, broadcasterID, token string) error
}

func revokedGrant(err error) bool {
	var ue *core.UpstreamError
	return errors.As(err, &ue) && ue.Status == http.StatusBadRequest && ue.Message == "invalid_grant"
}

func (p *api) reportRefreshFailure(ctx context.Context, broadcaster, token string, err error) {
	if !revokedGrant(err) {
		return
	}
	m, ok := p.keys.(keyDeadMarker)
	if !ok {
		return
	}
	if markErr := m.MarkDead(ctx, broadcaster, token); markErr != nil {
		monitor.TxnLogger(ctx, p.log).Warn("spotify grant is revoked; could not record it in custody",
			zap.String("broadcaster", broadcaster), zap.Error(markErr))
	}
}
