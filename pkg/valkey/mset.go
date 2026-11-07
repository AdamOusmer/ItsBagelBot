// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package valkey

import (
	"context"
	"time"

	valkey_go "github.com/valkey-io/valkey-go"
)

// MSet is one key/value/TTL triple for a pipelined write.
type MSet struct {
	Key   string
	Value string
	TTL   time.Duration
}

// SetAll writes every item as SET...PX in one pipeline, failing on the first error.
func SetAll(ctx context.Context, client valkey_go.Client, items []MSet) error {
	cmds := make(valkey_go.Commands, len(items))
	for i, it := range items {
		cmds[i] = client.B().Set().Key(it.Key).Value(it.Value).Px(it.TTL).Build()
	}
	for _, r := range client.DoMulti(ctx, cmds...) {
		if err := r.Error(); err != nil {
			return err
		}
	}
	return nil
}
