// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package valkey

import (
	"context"
	"fmt"
	"strconv"
	"time"

	valkey_go "github.com/valkey-io/valkey-go"
)

// The deadline check matters: once it expires the pool is about to be snapshotted.
var gatedJoinScript = valkey_go.NewLuaScript(`
if redis.call('EXISTS', KEYS[1]) == 0 or redis.call('EXISTS', KEYS[2]) == 0 then
  return {0, 0, redis.call('ZCARD', KEYS[3])}
end
local added = redis.call('ZADD', KEYS[3], 'NX', ARGV[1], ARGV[2])
redis.call('HSET', KEYS[4], ARGV[2], ARGV[3])
redis.call('EXPIRE', KEYS[3], ARGV[4])
redis.call('EXPIRE', KEYS[4], ARGV[4])
redis.call('EXPIRE', KEYS[1], ARGV[4])
return {1, added, redis.call('ZCARD', KEYS[3])}
`)

// GatedJoin adds a scored member to a set only while two guard keys both exist, recording a label alongside and refreshing every key's TTL.
type GatedJoin struct {
	GuardA, GuardB string
	Set            string
	Labels         string
}

// Join reports whether the gate was open, whether member was newly added, and the set's size afterward.
func (g GatedJoin) Join(ctx context.Context, client valkey_go.Client, score int64, member, label string, ttl time.Duration) (open, added bool, count int64, err error) {
	reply, err := gatedJoinScript.Exec(ctx, client,
		[]string{g.GuardA, g.GuardB, g.Set, g.Labels},
		[]string{
			strconv.FormatInt(score, 10), member, label,
			strconv.FormatInt(int64(ttl.Seconds()), 10),
		},
	).AsIntSlice()
	if err != nil {
		return false, false, 0, err
	}
	if len(reply) != 3 {
		return false, false, 0, fmt.Errorf("valkey: gated join returned %d values", len(reply))
	}
	return reply[0] == 1, reply[1] > 0, reply[2], nil
}
