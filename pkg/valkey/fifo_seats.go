// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package valkey

import (
	"context"
	"strconv"
	"time"

	valkey_go "github.com/valkey-io/valkey-go"
)

// Server time so replicas with skewed clocks agree on which leases have lapsed.
// A pinned seat never lapses: it passes to whichever holder picks it up, and FIFO stays.
const fifoSeatReap = `
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

var enterFIFOSeatScript = valkey_go.NewLuaScript(fifoSeatReap + `
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

var touchFIFOSeatScript = valkey_go.NewLuaScript(fifoSeatReap + `
if not redis.call('ZSCORE', KEYS[1], ARGV[1]) then
  return -1
end
return hold()
`)

var leaveFIFOSeatScript = valkey_go.NewLuaScript(`
if redis.call('HGET', KEYS[3], ARGV[1]) then
  return 0
end
redis.call('ZREM', KEYS[1], ARGV[1])
redis.call('ZREM', KEYS[2], ARGV[1])
return 1
`)

// FIFOSeats is a leased first-come line. Pins is a hash whose fields are tickets that keep their seat past a lapsed lease.
type FIFOSeats struct {
	Order  string
	Leases string
	Pins   string
}

// SeatRequest asks for one ticket's seat; PinnedTTL is how long the line lives while anything is pinned.
type SeatRequest struct {
	Ticket    string
	Lease     time.Duration
	Capacity  int
	PinnedTTL time.Duration
}

func (s FIFOSeats) keys() []string { return []string{s.Order, s.Leases, s.Pins} }

func (r SeatRequest) args() []string {
	return []string{r.Ticket, strconv.FormatInt(r.Lease.Milliseconds(), 10), strconv.Itoa(r.Capacity), strconv.FormatInt(r.PinnedTTL.Milliseconds(), 10)}
}

// Enter returns the ticket's place in line, or -1 when the line is full.
func (s FIFOSeats) Enter(ctx context.Context, client valkey_go.Client, req SeatRequest) (int64, error) {
	return enterFIFOSeatScript.Exec(ctx, client, s.keys(), req.args()).AsInt64()
}

// Touch renews the ticket's lease and returns its place, or -1 when it has no seat.
func (s FIFOSeats) Touch(ctx context.Context, client valkey_go.Client, req SeatRequest) (int64, error) {
	return touchFIFOSeatScript.Exec(ctx, client, s.keys(), req.args()).AsInt64()
}

// Leave gives up the seat unless the ticket is pinned.
func (s FIFOSeats) Leave(ctx context.Context, client valkey_go.Client, ticket string) error {
	return leaveFIFOSeatScript.Exec(ctx, client, s.keys(), []string{ticket}).Error()
}
