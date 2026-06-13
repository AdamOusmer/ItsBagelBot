// Package ratelimit implements a Valkey-backed token bucket. Capacity and
// refill rate are properties of the bucket, not the caller: every caller of
// the same key must pass the same Bucket, otherwise one caller's lower
// capacity would clamp away tokens another caller is entitled to.
package ratelimit

import (
	"context"
	"math"
	"strconv"
	"time"

	"github.com/valkey-io/valkey-go"
)

// luaTokenBucket refills with millisecond precision and float tokens, so
// fractional rates like 20 tokens per 30 seconds work exactly instead of
// being floored to whole tokens per second.
// KEYS[1]: bucket key
// ARGV[1]: capacity
// ARGV[2]: refill rate in tokens per millisecond
// ARGV[3]: current timestamp in milliseconds
// ARGV[4]: key TTL in seconds
// Returns 1 if a token was consumed, 0 if the bucket is empty.
const luaTokenBucket = `
local key = KEYS[1]
local capacity = tonumber(ARGV[1])
local refill_per_ms = tonumber(ARGV[2])
local now_ms = tonumber(ARGV[3])
local ttl_s = tonumber(ARGV[4])

local bucket = redis.call("HMGET", key, "tokens", "last_ms")
local tokens = tonumber(bucket[1])
local last_ms = tonumber(bucket[2])

if not tokens or not last_ms then
    tokens = capacity
    last_ms = now_ms
end

local elapsed = math.max(0, now_ms - last_ms)
tokens = math.min(capacity, tokens + (elapsed * refill_per_ms))

local allowed = 0
if tokens >= 1 then
    tokens = tokens - 1
    allowed = 1
end

redis.call("HSET", key, "tokens", tokens, "last_ms", now_ms)
redis.call("EXPIRE", key, ttl_s)
return allowed
`

// Bucket describes one token bucket. Capacity is the burst size,
// RefillPerSecond the sustained rate; both may be fractional.
type Bucket struct {
	Key             string
	Capacity        float64
	RefillPerSecond float64
}

type Limiter struct {
	client valkey.Client
	script *valkey.Lua
}

func New(client valkey.Client) *Limiter {
	return &Limiter{
		client: client,
		script: valkey.NewLuaScript(luaTokenBucket),
	}
}

// Allow consumes one token from the bucket and reports whether it was
// available. The bucket key expires after twice the time a full refill
// takes, so idle buckets clean themselves up.
func (l *Limiter) Allow(ctx context.Context, b Bucket) (bool, error) {

	now := time.Now().UnixMilli()
	ttl := int64(math.Ceil(b.Capacity/b.RefillPerSecond)) * 2

	res, err := l.script.Exec(ctx, l.client,
		[]string{b.Key},
		[]string{
			strconv.FormatFloat(b.Capacity, 'f', -1, 64),
			strconv.FormatFloat(b.RefillPerSecond/1000.0, 'g', -1, 64),
			strconv.FormatInt(now, 10),
			strconv.FormatInt(ttl, 10),
		},
	).AsInt64()
	if err != nil {
		return false, err
	}

	return res == 1, nil
}
