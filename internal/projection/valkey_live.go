// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package projection

import (
	"ItsBagelBot/internal/domain/event/data"
	"context"
	"fmt"
	"slices"
	"strconv"
	"time"

	pkg_valkey "ItsBagelBot/pkg/valkey"

	"github.com/valkey-io/valkey-go"
)

// web/dashboard/src/lib/server/live-counters.ts reads these keys; change both together.
const (
	liveCounterPrefix     = "ctr:live:"
	liveBoardPrefix       = "ctr:board:v2:"
	liveBoardMemberPrefix = "ctr:board-member:v2:"
	liveBoardSeedFlag     = "ctr:board-seeded:v2:"
	liveSeenPrefix        = "ctr:seen:"
	liveSeededAtField     = "seeded_at"
	liveSeenTTLSeconds    = "900"
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
	code, err := pkg_valkey.RunDecimalLedgerApply(ctx, v.primary, keys, args).AsInt64()
	if err != nil {
		return LiveSkipped, err
	}
	return LiveOutcome(code), nil
}

func (v *Store) SeedLiveCounters(ctx context.Context, userID uint64, values []CounterValue, seededAt time.Time) error {
	for _, value := range values {
		if value.Value < 0 {
			return fmt.Errorf("negative live counter: %s", value.Name)
		}
	}
	defer segment(ctx, "EVALSHA")()

	ordered, boards := boardsFirst(values)
	keys := append([]string{liveCounterKey(userID)}, boards...)
	args := counterArgs([]string{strconv.FormatUint(userID, 10), strconv.FormatInt(seededAt.UnixMilli(), 10)}, ordered)
	return pkg_valkey.RunDecimalLedgerSeed(ctx, v.primary, keys, args).Error()
}

func (v *Store) GetLiveCounters(ctx context.Context, userID uint64, names []CounterName) (map[CounterName]int64, bool, error) {
	defer segment(ctx, "HMGET")()

	fields := []string{liveSeededAtField}
	for _, name := range names {
		fields = append(fields, string(name))
	}
	res, err := pkg_valkey.Exec1(ctx, v.primary, pkg_valkey.HMGetCmd(v.primary, liveCounterKey(userID), fields...)).ToArray()
	if err != nil {
		return nil, false, err
	}
	if _, err := res[0].ToString(); err != nil {
		return nil, false, nil
	}
	values := make(map[CounterName]int64, len(names))
	for i, name := range names {
		value, err := decodeLiveCounter(name, res[i+1])
		if err != nil {
			return nil, true, err
		}
		values[name] = value
	}
	return values, true, nil
}

func decodeLiveCounter(name CounterName, message valkey.ValkeyMessage) (int64, error) {
	raw, err := message.ToString()
	if valkey.IsValkeyNil(err) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	value, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || value < 0 {
		return 0, fmt.Errorf("invalid live counter %s: %q", name, raw)
	}
	return value, nil
}

func (v *Store) BoardSeeded(ctx context.Context, name CounterName) (bool, error) {
	n, err := pkg_valkey.Exec1(ctx, v.primary, pkg_valkey.ExistsCmd(v.primary, liveBoardSeedFlag+string(name), liveBoardKey(name))).AsInt64()
	return n == 2, err
}

// The member index keeps a channel's fresher total when an older board seed arrives.
// Reversed lexicographic order preserves the previous board's descending user-ID ties.
func (v *Store) SeedBoard(ctx context.Context, name CounterName, entries []BoardEntry) error {
	defer segment(ctx, "ZADD")()

	args := make([]string, 0, len(entries)*2)
	for _, e := range entries {
		if e.Value < 0 || e.Value > data.MaxCounter {
			return fmt.Errorf("invalid board value: %d", e.Value)
		}
		args = append(args, strconv.FormatUint(e.UserID, 10), strconv.FormatInt(e.Value, 10))
	}
	return pkg_valkey.RunDecimalLedgerSeedBoard(ctx, v.primary, []string{liveBoardKey(name), liveBoardMemberPrefix + string(name), liveBoardSeedFlag + string(name)}, args).Error()
}

func (v *Store) DeleteLiveCounters(ctx context.Context, userID uint64, boards []CounterName) error {
	defer segment(ctx, "DEL")()

	keys := []string{liveCounterKey(userID)}
	for _, name := range boards {
		keys = append(keys, liveBoardKey(name), liveBoardMemberPrefix+string(name))
	}
	return pkg_valkey.RunDecimalLedgerDelete(ctx, v.primary, keys, []string{strconv.FormatUint(userID, 10)}).Error()
}
