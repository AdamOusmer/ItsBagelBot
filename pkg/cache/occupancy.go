package cache

import (
	"context"
	"time"

	"go.uber.org/zap"
)

// OccupancySource reports how full a cache runs: its live entry count against
// the ceiling it was built with. Both *Cache and *Keyed satisfy it, so a service
// can log occupancy across a mix of caches without caring about their element
// types.
type OccupancySource interface {
	Len() int
	Capacity() int64
}

// LogOccupancy emits one structured line summarising the fill of every named
// cache: its live entry count and configured ceiling. It is a point-in-time
// snapshot with no goroutine of its own, so callers drive the cadence. Logging
// at info keeps the reading visible in production (where debug is dropped), which
// is where the working set that the capacities should be tuned to actually shows.
func LogOccupancy(log *zap.Logger, caches map[string]OccupancySource) {
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

// StartOccupancyLogger logs the occupancy of the given caches every interval
// until ctx is cancelled, running in its own goroutine. It is the opt-in an
// always-on service wires in main to observe how full its in-process caches run
// over time, so the per-cache capacities can be tuned to the real working set.
// A non-positive interval disables it (no goroutine is started).
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
				LogOccupancy(log, caches)
			}
		}
	}()
}
