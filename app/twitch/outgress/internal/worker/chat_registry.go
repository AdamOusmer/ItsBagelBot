// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package worker

import (
	"context"
	"time"

	"ItsBagelBot/internal/domain/rpc/manage"

	"go.uber.org/zap"
)

func (w *Worker) chatChannel(ctx context.Context, id string) (manage.Channel, bool) {
	readCtx, cancel := context.WithTimeout(ctx, 150*time.Millisecond)
	defer cancel()
	ch, found, err := w.registry.Get(readCtx, id)
	if err != nil {
		w.log.Warn("moderator cache unavailable; using non-mod chat limit", zap.Error(err))
		return manage.Channel{}, false
	}
	return ch, found
}
