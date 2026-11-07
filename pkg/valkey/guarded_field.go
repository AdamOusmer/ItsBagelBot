// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package valkey

import (
	"context"

	valkey_go "github.com/valkey-io/valkey-go"
)

var guardedFieldWriteScript = NewScript(`
if redis.call('HGET', KEYS[1], ARGV[1]) ~= ARGV[2] then return 0 end
redis.call('HSET', KEYS[1], ARGV[3], ARGV[4])
return 1`)

// GuardedFieldWrite sets Field on Key only while GuardField still equals the expected value.
type GuardedFieldWrite struct {
	Key        string
	GuardField string
	Field      string
}

func (w GuardedFieldWrite) Apply(ctx context.Context, client valkey_go.Client, expect, value string) (bool, error) {
	n, err := RunScripts(ctx, client, []ScriptCall{{
		Script: guardedFieldWriteScript,
		Key:    w.Key,
		Args:   []string{w.GuardField, expect, w.Field, value},
	}})[0].ToInt64()
	return n == 1, err
}
