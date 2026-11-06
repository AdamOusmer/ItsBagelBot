// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package valkey

import (
	"context"
	"strconv"
	"time"

	valkey_go "github.com/valkey-io/valkey-go"
)

// Lists the member first, dropping members already past their time, so a failed listing aborts before the key is written.
var indexedKeyWrite = NewScript(`
redis.call('ZREMRANGEBYSCORE', KEYS[1], '-inf', ARGV[1])
redis.call('ZADD', KEYS[1], ARGV[2], ARGV[3])
if redis.call('PEXPIRETIME', KEYS[1]) < tonumber(ARGV[2]) then
  redis.call('PEXPIREAT', KEYS[1], ARGV[2])
end
if ARGV[5] ~= '' then
  return redis.call('SET', KEYS[2], ARGV[4], 'NX', 'GET', 'PX', ARGV[5])
end
if redis.call('PEXPIRETIME', KEYS[2]) >= tonumber(ARGV[2]) then return 0 end
redis.call('SET', KEYS[2], ARGV[4], 'PXAT', ARGV[2])
return 1`)

// IndexedKey is a key listed in an expiring index, so an owner's keys can be found without a keyspace scan. Index and Key must share a hash slot.
type IndexedKey struct {
	Index       string
	Member      string
	Key         string
	Value       string
	ExpiresAtMS int64
}

func (k IndexedKey) call(now time.Time, ttl string) ScriptCall {
	return ScriptCall{
		Script: indexedKeyWrite,
		Key:    k.Index,
		Also:   []string{k.Key},
		Args:   []string{strconv.FormatInt(now.UnixMilli(), 10), strconv.FormatInt(k.ExpiresAtMS, 10), k.Member, k.Value, ttl},
	}
}

// SetIfAbsent lists the key and writes it for ttl unless it exists, and returns the value it found.
func (k IndexedKey) SetIfAbsent(ctx context.Context, client valkey_go.Client, ttl time.Duration) (string, bool, error) {
	call := k.call(time.Now(), strconv.FormatInt(ttl.Milliseconds(), 10))
	held, err := RunScripts(ctx, client, []ScriptCall{call})[0].ToString()
	if valkey_go.IsValkeyNil(err) {
		return "", false, nil
	}
	return held, err == nil, err
}

// HoldIndexedKeys lists each key and keeps it until its expiry, never shortening a key already reaching that far.
func HoldIndexedKeys(ctx context.Context, client valkey_go.Client, keys []IndexedKey) error {
	now := time.Now()
	calls := make([]ScriptCall, len(keys))
	for i, key := range keys {
		calls[i] = key.call(now, "")
	}
	for _, result := range RunScripts(ctx, client, calls) {
		if err := result.Error(); err != nil {
			return err
		}
	}
	return nil
}

// IndexErase names one index and how its members map to their keys.
type IndexErase struct {
	Index string
	Batch int
	KeyOf func(member string) string
}

// Run unlinks every listed key in batches until the index is empty or ctx ends.
func (e IndexErase) Run(ctx context.Context, client valkey_go.Client) error {
	for ctx.Err() == nil {
		cmd := client.B().Zrange().Key(e.Index).Min("0").Max(strconv.Itoa(e.Batch - 1)).Build()
		members, err := client.Do(ctx, cmd).AsStrSlice()
		if err != nil || len(members) == 0 {
			return err
		}
		if err := e.eraseBatch(ctx, client, members); err != nil {
			return err
		}
	}
	return ctx.Err()
}

func (e IndexErase) eraseBatch(ctx context.Context, client valkey_go.Client, members []string) error {
	keys := make([]string, len(members))
	for i, member := range members {
		keys[i] = e.KeyOf(member)
	}
	unlink := client.B().Unlink().Key(keys...).Build()
	forget := client.B().Zrem().Key(e.Index).Member(members...).Build()
	for _, result := range client.DoMulti(ctx, unlink, forget) {
		if err := result.Error(); err != nil {
			return err
		}
	}
	return nil
}
