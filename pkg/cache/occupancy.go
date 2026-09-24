// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package cache

import (
	"context"
	"time"

	"go.uber.org/zap"
)

type OccupancySource interface {
	Len() int
	Capacity() int64
}

func logOccupancy(log *zap.Logger, caches map[string]OccupancySource) {
	if log == nil || len(caches) == 0 {
		return
	}
	fields := make([]zap.Field, 0, len(caches)*2)
	for name, c := range caches {
		fields = append(fields,
			zap.Int(name+"_entries", c.Len()),
			zap.Int64(name+"_capacity", c.Capacity()),
		)
	}
	log.Info("cache occupancy", fields...)
}

func StartOccupancyLogger(ctx context.Context, log *zap.Logger, interval time.Duration, caches map[string]OccupancySource) {
	if interval <= 0 {
		return
	}
	if log == nil || len(caches) == 0 {
		return
	}
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				logOccupancy(log, caches)
			}
		}
	}()
}
