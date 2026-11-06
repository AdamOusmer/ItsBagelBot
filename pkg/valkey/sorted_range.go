// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package valkey

import (
	"context"
	"strconv"

	valkey_go "github.com/valkey-io/valkey-go"
)

// ZRangeByIndex returns the members ranked start..stop inclusive.
func ZRangeByIndex(ctx context.Context, client valkey_go.Client, key string, start, stop int64) ([]string, error) {
	return client.Do(ctx, client.B().Zrange().Key(key).Min(strconv.FormatInt(start, 10)).Max(strconv.FormatInt(stop, 10)).Build()).AsStrSlice()
}

// ZRangeByScoreLimit returns up to count members scored between min and max, starting at offset.
func ZRangeByScoreLimit(ctx context.Context, client valkey_go.Client, key, min, max string, offset, count int64) ([]string, error) {
	return client.Do(ctx, client.B().Zrangebyscore().Key(key).Min(min).Max(max).Limit(offset, count).Build()).AsStrSlice()
}
