// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"context"
	"strconv"
	"time"

	pkg_valkey "ItsBagelBot/pkg/valkey"

	"github.com/valkey-io/valkey-go"
)

const (
	runQueueLease     = 15 * time.Second
	runQueueCapacity  = maxQueueWaiting + 1
	runQueueKeyPrefix = "automation:queue:"
)

// Server time so replicas with skewed clocks agree on which leases have lapsed.
const runQueueReap = `
local t = redis.call('TIME')
local now = t[1] * 1000 + math.floor(t[2] / 1000)
local lease = tonumber(ARGV[2])
local lapsed = redis.call('ZRANGEBYSCORE', KEYS[2], '-inf', now)
if #lapsed > 0 then
  redis.call('ZREM', KEYS[1], unpack(lapsed))
  redis.call('ZREM', KEYS[2], unpack(lapsed))
end
local function hold()
  redis.call('ZADD', KEYS[2], now + lease, ARGV[1])
  redis.call('PEXPIRE', KEYS[1], lease * 2)
  redis.call('PEXPIRE', KEYS[2], lease * 2)
  return redis.call('ZRANK', KEYS[1], ARGV[1])
end
`

var enterRunQueueScript = valkey.NewLuaScript(runQueueReap + `
if redis.call('ZSCORE', KEYS[1], ARGV[1]) then
  return hold()
end
if redis.call('ZCARD', KEYS[1]) >= tonumber(ARGV[3]) then
  return -1
end
local last = redis.call('ZREVRANGE', KEYS[1], 0, 0, 'WITHSCORES')
local seq = 1
if #last > 0 then
  seq = tonumber(last[2]) + 1
end
redis.call('ZADD', KEYS[1], seq, ARGV[1])
return hold()
`)

var touchRunQueueScript = valkey.NewLuaScript(runQueueReap + `
if not redis.call('ZSCORE', KEYS[1], ARGV[1]) then
  return -1
end
return hold()
`)

type ValkeyRunQueue struct {
	client valkey.Client
	lease  time.Duration
}

func NewValkeyRunQueue(client valkey.Client) *ValkeyRunQueue {
	return newValkeyRunQueue(client, runQueueLease)
}

func newValkeyRunQueue(client valkey.Client, lease time.Duration) *ValkeyRunQueue {
	return &ValkeyRunQueue{client: pkg_valkey.Primary(client), lease: lease}
}

func runQueueKeys(seat RunQueueSeat) []string {
	base := runQueueKeyPrefix + "{" + strconv.FormatUint(seat.BroadcasterID, 10) + "}:" + seat.Queue
	return []string{base + ":order", base + ":lease"}
}

func (q *ValkeyRunQueue) args(seat RunQueueSeat) []string {
	return []string{seat.Ticket, strconv.FormatInt(q.lease.Milliseconds(), 10), strconv.Itoa(runQueueCapacity)}
}

func (q *ValkeyRunQueue) Enter(ctx context.Context, seat RunQueueSeat) (int64, error) {
	return enterRunQueueScript.Exec(ctx, q.client, runQueueKeys(seat), q.args(seat)).AsInt64()
}

func (q *ValkeyRunQueue) Touch(ctx context.Context, seat RunQueueSeat) (int64, error) {
	return touchRunQueueScript.Exec(ctx, q.client, runQueueKeys(seat), q.args(seat)).AsInt64()
}

func (q *ValkeyRunQueue) Leave(ctx context.Context, seat RunQueueSeat) error {
	keys := runQueueKeys(seat)
	for _, res := range q.client.DoMulti(ctx,
		q.client.B().Zrem().Key(keys[0]).Member(seat.Ticket).Build(),
		q.client.B().Zrem().Key(keys[1]).Member(seat.Ticket).Build(),
	) {
		if err := res.Error(); err != nil {
			return err
		}
	}
	return nil
}
