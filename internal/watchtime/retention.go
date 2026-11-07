// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package watchtime

import (
	"context"
	"strconv"
	"strings"
	"time"

	"ItsBagelBot/internal/domain/event/data"
	pkgvalkey "ItsBagelBot/pkg/valkey"
)

const (
	HistoryRetention  = 7 * 24 * time.Hour
	OutboxWindowIndex = "watchtime:outbox:windows"
	RetentionFloor    = "watchtime:retention:floor"
)

// WindowStartUnixMilli preserves legacy windows whose collection timestamp
// predates the explicit field. Unknown ages remain zero and block pruning.
func WindowStartUnixMilli(a data.WatchAwardDTO) int64 {
	if a.WindowStartedAtUnixMilli > 0 {
		return a.WindowStartedAtUnixMilli
	}
	_, last, found := strings.Cut(a.WindowID, ":")
	if !found {
		return 0
	}
	parts := strings.Split(last, ":")
	start, _ := strconv.ParseInt(parts[len(parts)-1], 10, 64)
	return max(0, start)
}

// SafePruneBefore publishes the acceptance barrier before SQL pruning. Every
// queued award pins it below that window's start, including pending deliveries.
// Unindexed legacy deliveries conservatively prevent advancement until drained.
func (s *Store) SafePruneBefore(ctx context.Context, desiredUnixMilli int64) (int64, error) {
	keys := []string{Stream, OutboxWindowIndex, RetentionFloor}
	args := []string{strconv.FormatInt(desiredUnixMilli, 10), strconv.FormatInt(HistoryRetention.Milliseconds(), 10)}
	return pkgvalkey.RunOutboxBarrierAdvance(ctx, s.client, keys, args).AsInt64()
}
