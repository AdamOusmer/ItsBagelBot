// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package valkey

import (
	"context"
	"math/rand/v2"
	"strconv"
	"time"

	valkey_go "github.com/valkey-io/valkey-go"
)

type RecentLog struct {
	Key  string
	Keep int
	TTL  time.Duration
}

func RecordRecent(ctx context.Context, client valkey_go.Client, log RecentLog, at time.Time) error {
	ms := at.UnixMilli()
	member := strconv.FormatInt(ms, 10) + "-" + strconv.FormatUint(rand.Uint64(), 36)
	resps := client.DoMulti(ctx,
		client.B().Zadd().Key(log.Key).ScoreMember().ScoreMember(float64(ms), member).Build(),
		client.B().Zremrangebyrank().Key(log.Key).Start(0).Stop(-int64(log.Keep)-1).Build(),
		client.B().Pexpire().Key(log.Key).Milliseconds(log.TTL.Milliseconds()).Build(),
	)
	for _, resp := range resps {
		if err := resp.Error(); err != nil {
			return err
		}
	}
	return nil
}

func CountRecentSince(ctx context.Context, client valkey_go.Client, key string, since time.Time) (int64, error) {
	return client.Do(ctx, client.B().Zcount().Key(key).Min(strconv.FormatInt(since.UnixMilli(), 10)).Max("+inf").Build()).AsInt64()
}
