// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package valkey

import (
	"context"
	"time"

	valkey_go "github.com/valkey-io/valkey-go"
)

// TouchAndCountHLL adds member to two HyperLogLog keys, refreshes both TTLs, and reads back each key's estimated
// cardinality, in one pipeline. Results come back in order: PFADD(a), PFADD(b), EXPIRE(a), EXPIRE(b), PFCOUNT(a), PFCOUNT(b).
func TouchAndCountHLL(ctx context.Context, client valkey_go.Client, keyA, keyB, member string, ttl time.Duration) []valkey_go.ValkeyResult {
	seconds := int64(ttl.Seconds())
	return client.DoMulti(ctx,
		client.B().Pfadd().Key(keyA).Element(member).Build(),
		client.B().Pfadd().Key(keyB).Element(member).Build(),
		client.B().Expire().Key(keyA).Seconds(seconds).Build(),
		client.B().Expire().Key(keyB).Seconds(seconds).Build(),
		client.B().Pfcount().Key(keyA).Build(),
		client.B().Pfcount().Key(keyB).Build(),
	)
}
