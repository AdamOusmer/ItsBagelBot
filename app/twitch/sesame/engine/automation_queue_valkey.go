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
// A seat with a durable record never lapses: its run passes to whichever replica resumes it, and FIFO stays.
const runQueueReap = `
local t = redis.call('TIME')
local now = t[1] * 1000 + math.floor(t[2] / 1000)
local lease = tonumber(ARGV[2])
local lapsed = redis.call('ZRANGEBYSCORE', KEYS[2], '-inf', now)
for _, ticket in ipairs(lapsed) do
  if not redis.call('HGET', KEYS[3], ticket) then
    redis.call('ZREM', KEYS[1], ticket)
    redis.call('ZREM', KEYS[2], ticket)
  end
end
local function hold()
  redis.call('ZADD', KEYS[2], now + lease, ARGV[1])
  local ttl = lease * 2
  if redis.call('EXISTS', KEYS[3]) == 1 then
    ttl = math.max(ttl, tonumber(ARGV[4]))
  end
  redis.call('PEXPIRE', KEYS[1], ttl)
  redis.call('PEXPIRE', KEYS[2], ttl)
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

var leaveRunQueueScript = valkey.NewLuaScript(`
if redis.call('HGET', KEYS[3], ARGV[1]) then
  return 0
end
redis.call('ZREM', KEYS[1], ARGV[1])
redis.call('ZREM', KEYS[2], ARGV[1])
return 1
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

func runQueueScriptKeys(seat RunQueueSeat) []string {
	return append(runQueueKeys(seat), durableSeatsKey(seat.BroadcasterID))
}

func (q *ValkeyRunQueue) args(seat RunQueueSeat) []string {
	return []string{seat.Ticket, strconv.FormatInt(q.lease.Milliseconds(), 10), strconv.Itoa(runQueueCapacity), durableTTLMillis()}
}

func (q *ValkeyRunQueue) Enter(ctx context.Context, seat RunQueueSeat) (int64, error) {
	return enterRunQueueScript.Exec(ctx, q.client, runQueueScriptKeys(seat), q.args(seat)).AsInt64()
}

func (q *ValkeyRunQueue) Touch(ctx context.Context, seat RunQueueSeat) (int64, error) {
	return touchRunQueueScript.Exec(ctx, q.client, runQueueScriptKeys(seat), q.args(seat)).AsInt64()
}

// A path still recorded for this ticket keeps the seat: the last one to finish lets it go.
func (q *ValkeyRunQueue) Leave(ctx context.Context, seat RunQueueSeat) error {
	return leaveRunQueueScript.Exec(ctx, q.client, runQueueScriptKeys(seat), []string{seat.Ticket}).Error()
}
