// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package projection

import (
	"context"
	"slices"
	"strconv"
	"time"

	"github.com/valkey-io/valkey-go"
)

// web/dashboard/src/lib/server/live-counters.ts reads these keys; change both together.
const (
	liveCounterPrefix  = "ctr:live:"
	liveBoardPrefix    = "ctr:board:"
	liveBoardSeedFlag  = "ctr:board-seeded:"
	liveSeenPrefix     = "ctr:seen:"
	liveSeededAtField  = "seeded_at"
	liveSeenTTLSeconds = "900"
)

type LiveOutcome int

const (
	LiveApplied LiveOutcome = iota
	LiveSkipped
	LiveNeedsSeed
)

type CounterName string

type CounterValue struct {
	Name  CounterName
	Value int64
	Board bool
}

type LiveBatch struct {
	UserID   uint64
	MsgID    string
	StoredAt time.Time
	Deltas   []CounterValue
}

type BoardEntry struct {
	UserID uint64
	Value  int64
}

var applyLiveScript = valkey.NewLuaScript(`
local seeded = redis.call('HGET', KEYS[1], 'seeded_at')
if not seeded then return 2 end
if tonumber(ARGV[2]) < tonumber(seeded) then return 1 end
if not redis.call('SET', KEYS[2], '1', 'NX', 'EX', ARGV[3]) then return 1 end
local i = 4
local n = 0
while ARGV[i] do
  local total = redis.call('HINCRBY', KEYS[1], ARGV[i], ARGV[i + 1])
  n = n + 1
  if KEYS[2 + n] then redis.call('ZADD', KEYS[2 + n], total, ARGV[1]) end
  i = i + 2
end
return 0`)

var seedLiveScript = valkey.NewLuaScript(`
if redis.call('HEXISTS', KEYS[1], 'seeded_at') == 1 then return 0 end
local i = 3
local n = 0
while ARGV[i] do
  redis.call('HSET', KEYS[1], ARGV[i], ARGV[i + 1])
  n = n + 1
  if KEYS[1 + n] then redis.call('ZADD', KEYS[1 + n], ARGV[i + 1], ARGV[1]) end
  i = i + 2
end
redis.call('HSET', KEYS[1], 'seeded_at', ARGV[2])
return 1`)

func liveCounterKey(userID uint64) string { return liveCounterPrefix + strconv.FormatUint(userID, 10) }

func liveBoardKey(name CounterName) string { return liveBoardPrefix + string(name) }

func boardsFirst(values []CounterValue) ([]CounterValue, []string) {
	ordered := slices.Clone(values)
	slices.SortStableFunc(ordered, func(a, b CounterValue) int {
		switch {
		case a.Board == b.Board:
			return 0
		case a.Board:
			return -1
		default:
			return 1
		}
	})
	var boards []string
	for _, v := range ordered {
		if v.Board {
			boards = append(boards, liveBoardKey(v.Name))
		}
	}
	return ordered, boards
}

func counterArgs(head []string, values []CounterValue) []string {
	args := slices.Clone(head)
	for _, v := range values {
		args = append(args, string(v.Name), strconv.FormatInt(v.Value, 10))
	}
	return args
}

// Batches stored before the seed are already in the seeded value, so they are skipped rather than counted twice.
func (v *Store) ApplyLiveCounters(ctx context.Context, b LiveBatch) (LiveOutcome, error) {
	defer segment(ctx, "EVALSHA")()

	ordered, boards := boardsFirst(b.Deltas)
	keys := append([]string{liveCounterKey(b.UserID), liveSeenPrefix + b.MsgID}, boards...)
	args := counterArgs([]string{strconv.FormatUint(b.UserID, 10), strconv.FormatInt(b.StoredAt.UnixMilli(), 10), liveSeenTTLSeconds}, ordered)
	code, err := applyLiveScript.Exec(ctx, v.primary, keys, args).AsInt64()
	if err != nil {
		return LiveSkipped, err
	}
	return LiveOutcome(code), nil
}

func (v *Store) SeedLiveCounters(ctx context.Context, userID uint64, values []CounterValue, seededAt time.Time) error {
	defer segment(ctx, "EVALSHA")()

	ordered, boards := boardsFirst(values)
	keys := append([]string{liveCounterKey(userID)}, boards...)
	args := counterArgs([]string{strconv.FormatUint(userID, 10), strconv.FormatInt(seededAt.UnixMilli(), 10)}, ordered)
	return seedLiveScript.Exec(ctx, v.primary, keys, args).Error()
}

func (v *Store) GetLiveCounters(ctx context.Context, userID uint64, names []CounterName) (map[CounterName]int64, bool, error) {
	defer segment(ctx, "HMGET")()

	fields := []string{liveSeededAtField}
	for _, name := range names {
		fields = append(fields, string(name))
	}
	res, err := v.primary.Do(ctx, v.primary.B().Hmget().Key(liveCounterKey(userID)).Field(fields...).Build()).ToArray()
	if err != nil {
		return nil, false, err
	}
	if _, err := res[0].ToString(); err != nil {
		return nil, false, nil
	}
	values := make(map[CounterName]int64, len(names))
	for i, name := range names {
		raw, _ := res[i+1].ToString()
		values[name], _ = strconv.ParseInt(raw, 10, 64)
	}
	return values, true, nil
}

func (v *Store) BoardSeeded(ctx context.Context, name CounterName) (bool, error) {
	n, err := v.primary.Do(ctx, v.primary.B().Exists().Key(liveBoardSeedFlag+string(name)).Build()).AsInt64()
	return n > 0, err
}

// NX keeps a channel's seeded or live total when the older board read lands after it.
func (v *Store) SeedBoard(ctx context.Context, name CounterName, entries []BoardEntry) error {
	defer segment(ctx, "ZADD")()

	cmds := make([]valkey.Completed, 0, len(entries)+1)
	for _, e := range entries {
		cmds = append(cmds, v.client.B().Zadd().Key(liveBoardKey(name)).Nx().ScoreMember().
			ScoreMember(float64(e.Value), strconv.FormatUint(e.UserID, 10)).Build())
	}
	cmds = append(cmds, v.client.B().Set().Key(liveBoardSeedFlag+string(name)).Value("1").Build())
	return v.pipeline(ctx, cmds...)
}

func (v *Store) DeleteLiveCounters(ctx context.Context, userID uint64, boards []CounterName) error {
	defer segment(ctx, "DEL")()

	cmds := []valkey.Completed{v.client.B().Del().Key(liveCounterKey(userID)).Build()}
	for _, name := range boards {
		cmds = append(cmds, v.client.B().Zrem().Key(liveBoardKey(name)).Member(strconv.FormatUint(userID, 10)).Build())
	}
	return v.pipeline(ctx, cmds...)
}
