// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package valkey

import (
	"context"
	"strconv"
	"time"

	valkey_go "github.com/valkey-io/valkey-go"
)

var (
	seededCounterKeyScript = valkey_go.NewLuaScript(`
if redis.call('EXISTS', KEYS[1]) == 0 then
  if ARGV[1] == '' then return false end
  redis.call('SET', KEYS[1], ARGV[1])
end
local value = redis.call('INCRBY', KEYS[1], ARGV[2])
redis.call('EXPIRE', KEYS[1], ARGV[3])
return value`)
	seededCounterFieldScript = valkey_go.NewLuaScript(`
if redis.call('HEXISTS', KEYS[1], ARGV[1]) == 0 then
  if ARGV[2] == '' then return false end
  redis.call('HSET', KEYS[1], ARGV[1], ARGV[2])
end
local value = redis.call('HINCRBY', KEYS[1], ARGV[1], ARGV[3])
redis.call('EXPIRE', KEYS[1], ARGV[4])
return value`)
)

// SeededCounter increments a counter that seeds itself from an external source of truth the first time it is touched.
// Field empty means a plain key counter; Field set means one field of a hash counter.
type SeededCounter struct {
	Key   string
	Field string
	TTL   time.Duration
}

// Bump increments the counter by delta, seeding it with seed() only when it must create the counter.
// seed is called at most once and its own error handling (if any) is the caller's responsibility.
func (c SeededCounter) Bump(ctx context.Context, client valkey_go.Client, delta int64, seed func() int64) (int64, error) {
	if c.Field == "" {
		return c.bumpKey(ctx, client, delta, seed)
	}
	return c.bumpField(ctx, client, delta, seed)
}

func (c SeededCounter) bumpKey(ctx context.Context, client valkey_go.Client, delta int64, seed func() int64) (int64, error) {
	deltaArg, ttlArg := strconv.FormatInt(delta, 10), strconv.FormatInt(int64(c.TTL.Seconds()), 10)
	value, err := seededCounterKeyScript.Exec(ctx, client, []string{c.Key}, []string{"", deltaArg, ttlArg}).AsInt64()
	if err == nil {
		return value, nil
	}
	if !valkey_go.IsValkeyNil(err) {
		return 0, err
	}
	seedArg := strconv.FormatInt(seed(), 10)
	return seededCounterKeyScript.Exec(ctx, client, []string{c.Key}, []string{seedArg, deltaArg, ttlArg}).AsInt64()
}

func (c SeededCounter) bumpField(ctx context.Context, client valkey_go.Client, delta int64, seed func() int64) (int64, error) {
	deltaArg, ttlArg := strconv.FormatInt(delta, 10), strconv.FormatInt(int64(c.TTL.Seconds()), 10)
	value, err := seededCounterFieldScript.Exec(ctx, client, []string{c.Key}, []string{c.Field, "", deltaArg, ttlArg}).AsInt64()
	if err == nil {
		return value, nil
	}
	if !valkey_go.IsValkeyNil(err) {
		return 0, err
	}
	seedArg := strconv.FormatInt(seed(), 10)
	return seededCounterFieldScript.Exec(ctx, client, []string{c.Key}, []string{c.Field, seedArg, deltaArg, ttlArg}).AsInt64()
}
