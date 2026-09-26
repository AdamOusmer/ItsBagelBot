// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package watchtime

import (
	"context"
	"strconv"
	"strings"
	"time"

	"ItsBagelBot/internal/domain/event/data"
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
	return s.client.Do(ctx, s.client.B().Eval().Script(`
local n=redis.call('XLEN',KEYS[1])
if n~=redis.call('ZCARD',KEYS[2]) then return 0 end
local clock=redis.call('TIME')
local now=tonumber(clock[1])*1000+math.floor(tonumber(clock[2])/1000)
local cutoff=math.min(tonumber(ARGV[1]),now-tonumber(ARGV[2]))
local first=redis.call('ZRANGE',KEYS[2],0,0,'WITHSCORES')
if #first>0 then cutoff=math.min(cutoff,tonumber(first[2])-1) end
if cutoff<=0 then return 0 end
local prior=tonumber(redis.call('GET',KEYS[3]) or '0')
if prior>cutoff then return 0 end
redis.call('SET',KEYS[3],string.format('%.0f',cutoff))
return cutoff`).Numkeys(3).Key(Stream, OutboxWindowIndex, RetentionFloor).
		Arg(strconv.FormatInt(desiredUnixMilli, 10), strconv.FormatInt(HistoryRetention.Milliseconds(), 10)).Build()).AsInt64()
}
