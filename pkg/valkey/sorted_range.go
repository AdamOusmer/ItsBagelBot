// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package valkey

import (
	"context"
	"strconv"

	valkey_go "github.com/valkey-io/valkey-go"
)

// RankRange names the members ranked Start..Stop inclusive.
type RankRange struct {
	Key   string
	Start int64
	Stop  int64
}

// ZRangeByIndex returns the members in r.
func ZRangeByIndex(ctx context.Context, client valkey_go.Client, r RankRange) ([]string, error) {
	return client.Do(ctx, client.B().Zrange().Key(r.Key).Min(strconv.FormatInt(r.Start, 10)).Max(strconv.FormatInt(r.Stop, 10)).Build()).AsStrSlice()
}

// ScorePage names up to Count members scored between Min and Max, starting at Offset.
type ScorePage struct {
	Key    string
	Min    string
	Max    string
	Offset int64
	Count  int64
}

// ZRangeByScoreLimit returns the members in p.
func ZRangeByScoreLimit(ctx context.Context, client valkey_go.Client, p ScorePage) ([]string, error) {
	return client.Do(ctx, client.B().Zrangebyscore().Key(p.Key).Min(p.Min).Max(p.Max).Limit(p.Offset, p.Count).Build()).AsStrSlice()
}
