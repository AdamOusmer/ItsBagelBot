// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package valkey

import (
	"context"
	"time"

	valkey_go "github.com/valkey-io/valkey-go"
)

// HLLBandTouch names the two HyperLogLog keys to touch together, the member to add, and the TTL to refresh both to.
type HLLBandTouch struct {
	KeyA, KeyB string
	Member     string
	TTL        time.Duration
}

// TouchAndCountHLL adds the member to two HyperLogLog keys, refreshes both TTLs, and reads back each key's estimated
// cardinality, in one pipeline. Results come back in order: PFADD(a), PFADD(b), EXPIRE(a), EXPIRE(b), PFCOUNT(a), PFCOUNT(b).
func TouchAndCountHLL(ctx context.Context, client valkey_go.Client, touch HLLBandTouch) []valkey_go.ValkeyResult {
	seconds := int64(touch.TTL.Seconds())
	return client.DoMulti(ctx,
		client.B().Pfadd().Key(touch.KeyA).Element(touch.Member).Build(),
		client.B().Pfadd().Key(touch.KeyB).Element(touch.Member).Build(),
		client.B().Expire().Key(touch.KeyA).Seconds(seconds).Build(),
		client.B().Expire().Key(touch.KeyB).Seconds(seconds).Build(),
		client.B().Pfcount().Key(touch.KeyA).Build(),
		client.B().Pfcount().Key(touch.KeyB).Build(),
	)
}
