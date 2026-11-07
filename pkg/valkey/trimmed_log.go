// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package valkey

import (
	"context"

	valkey_go "github.com/valkey-io/valkey-go"
)

// ScoreMember is one sorted-set entry for a batched append.
type ScoreMember struct {
	Score  float64
	Member string
}

// TrimmedLog is a sorted log kept to its newest Keep members, with members older than a batch's cutoff dropped and its TTL refreshed on every append.
type TrimmedLog struct {
	Key        string
	Keep       int64
	TTLSeconds int64
}

// AppendBatch adds entries, drops members scored at or below cutoffScore, caps the log to Keep by rank, and refreshes its TTL, in one pipeline.
func (l TrimmedLog) AppendBatch(ctx context.Context, client valkey_go.Client, entries []ScoreMember, cutoffScore string) []valkey_go.ValkeyResult {
	zadd := client.B().Zadd().Key(l.Key).ScoreMember()
	for _, e := range entries {
		zadd = zadd.ScoreMember(e.Score, e.Member)
	}
	return client.DoMulti(ctx,
		zadd.Build(),
		client.B().Zremrangebyscore().Key(l.Key).Min("-inf").Max(cutoffScore).Build(),
		client.B().Zremrangebyrank().Key(l.Key).Start(0).Stop(-(l.Keep + 1)).Build(),
		client.B().Expire().Key(l.Key).Seconds(l.TTLSeconds).Build(),
	)
}

// Range reads members scored at or above minScore, oldest first, up to limit.
func (l TrimmedLog) Range(ctx context.Context, client valkey_go.Client, minScore string, limit int64) ([]string, error) {
	return client.Do(ctx, client.B().Zrangebyscore().Key(l.Key).Min(minScore).Max("+inf").Limit(0, limit).Build()).AsStrSlice()
}
