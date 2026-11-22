// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package valkey

import (
	"context"
	"time"

	valkey_go "github.com/valkey-io/valkey-go"
)

// PTTLs reads each key's remaining lifetime in one round trip, in key order; a missing key or a key without expiry reads as a negative duration.
func PTTLs(ctx context.Context, client valkey_go.Client, keys []string) ([]time.Duration, error) {
	cmds := make(valkey_go.Commands, len(keys))
	for i, key := range keys {
		cmds[i] = client.B().Pttl().Key(key).Build()
	}
	out := make([]time.Duration, len(keys))
	for i, res := range client.DoMulti(ctx, cmds...) {
		ms, err := res.AsInt64()
		if err != nil {
			return nil, err
		}
		out[i] = time.Duration(ms) * time.Millisecond
	}
	return out, nil
}
