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

// Add scores member at at.UnixMilli and renews the key's TTL.
func (i TimeIndex) Add(ctx context.Context, client valkey_go.Client, at time.Time, member string, ttl time.Duration) error {
	resps := client.DoMulti(ctx,
		client.B().Zadd().Key(i.Key).ScoreMember().ScoreMember(float64(at.UnixMilli()), member).Build(),
		client.B().Expire().Key(i.Key).Seconds(int64(ttl.Seconds())).Build(),
	)
	return resps[0].Error()
}
