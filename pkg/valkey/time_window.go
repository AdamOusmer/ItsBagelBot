// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package valkey

import (
	"context"
	"strconv"
	"time"

	valkey_go "github.com/valkey-io/valkey-go"
)

// TimeIndex is a sorted set scored by UnixMilli, trimmed to a floor on every read.
type TimeIndex struct{ Key string }

// Since prunes members scored before since, then returns the remaining members with their times.
func (i TimeIndex) Since(ctx context.Context, client valkey_go.Client, since time.Time) ([]valkey_go.ZScore, error) {
	floor := strconv.FormatInt(since.UnixMilli(), 10)
	resps := client.DoMulti(ctx,
		client.B().Zremrangebyscore().Key(i.Key).Min("-inf").Max("("+floor).Build(),
		client.B().Zrangebyscore().Key(i.Key).Min(floor).Max("+inf").Withscores().Build(),
	)
	if err := resps[0].Error(); err != nil {
		return nil, err
	}
	return resps[1].AsZScores()
}

// TimedEntry is one member of a TimeIndex and the TTL its write renews.
type TimedEntry struct {
	At     time.Time
	Member string
	TTL    time.Duration
}

// Add scores the entry's member at At.UnixMilli and renews the key's TTL.
func (i TimeIndex) Add(ctx context.Context, client valkey_go.Client, entry TimedEntry) error {
	resps := client.DoMulti(ctx,
		client.B().Zadd().Key(i.Key).ScoreMember().ScoreMember(float64(entry.At.UnixMilli()), entry.Member).Build(),
		client.B().Expire().Key(i.Key).Seconds(int64(entry.TTL.Seconds())).Build(),
	)
	return resps[0].Error()
}
