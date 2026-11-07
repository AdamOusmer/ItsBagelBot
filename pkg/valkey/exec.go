// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package valkey

import (
	"context"

	valkey_go "github.com/valkey-io/valkey-go"
)

// Exec1 runs one already-built command and returns its raw result for the
// caller to decode, exactly as a direct Do call would.
func Exec1(ctx context.Context, client valkey_go.Client, cmd valkey_go.Completed) valkey_go.ValkeyResult {
	return client.Do(ctx, cmd)
}

// ExecMulti runs every command in one pipeline and returns the raw per-command
// results for the caller to decode, exactly as a direct DoMulti call would.
func ExecMulti(ctx context.Context, client valkey_go.Client, cmds ...valkey_go.Completed) []valkey_go.ValkeyResult {
	return client.DoMulti(ctx, cmds...)
}
