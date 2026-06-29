// Package ratelimit implements a Valkey-backed token bucket. Capacity and
// refill rate are properties of the bucket, not the caller: every caller of
// the same key must use the same Spec, otherwise one caller's lower capacity
// would clamp away tokens another caller is entitled to.
package ratelimit

import (
	"context"
	"errors"
	"math"
	"strconv"

	"github.com/valkey-io/valkey-go"
)

// Native Valkey primitives are preferred throughout outgress. A token bucket
// is the narrow exception: pipelines cannot conditionally skip the shared
// bucket, MULTI/EXEC has no branching, and WATCH adds read/transaction RTTs and
// contention retries. One bounded script is therefore the only native Valkey
// mechanism that preserves the ordered two-bucket invariant in one RTT.
//
// The script evaluates one or two buckets in order. It reads every participating
// key before its first write, so a wrong-type error on bucket two cannot leave
// bucket one partially consumed. Valkey TIME supplies one authoritative clock
// for the fleet. The return value is zero on success or the one-based index of
// the first denied bucket.
//
// KEYS: bucket keys, one or two.
// ARGV: capacity, refill-per-millisecond, TTL-seconds for each key.
const luaOrderedTokenBucket = `
local count = #KEYS
if count < 1 or count > 2 then
    return redis.error_reply("token bucket requires one or two keys")
end
if #ARGV ~= count * 3 then
    return redis.error_reply("invalid token bucket arguments")
end

local server_time = redis.call("TIME")
local now_ms = (tonumber(server_time[1]) * 1000) + math.floor(tonumber(server_time[2]) / 1000)
local states = {}

-- Read and validate every key before writing either key. redis.pcall turns a
-- WRONGTYPE into a value we can return before any state has changed.
for i = 1, count do
    local offset = ((i - 1) * 3)
    local capacity = tonumber(ARGV[offset + 1])
    local refill_per_ms = tonumber(ARGV[offset + 2])
    local ttl_s = tonumber(ARGV[offset + 3])
    if not capacity or capacity <= 0 or not refill_per_ms or refill_per_ms <= 0 or not ttl_s or ttl_s <= 0 then
        return redis.error_reply("invalid token bucket spec")
    end

    local bucket = redis.pcall("HMGET", KEYS[i], "tokens", "last_ms")
    if bucket.err then
        return bucket
    end

    local tokens = tonumber(bucket[1])
    local last_ms = tonumber(bucket[2])
    if not tokens or not last_ms then
        tokens = capacity
        last_ms = now_ms
    end

    local elapsed = math.max(0, now_ms - last_ms)
    tokens = math.min(capacity, tokens + (elapsed * refill_per_ms))
    states[i] = {tokens = tokens, ttl_s = ttl_s}
end

local denied = 0
for i = 1, count do
    if states[i].tokens < 1 then
        denied = i
        break
    end
end

if denied ~= 0 then
    -- Atomic evaluation: if any bucket denies, do not consume tokens from any bucket.
    return denied
end

for i = 1, count do
    states[i].tokens = states[i].tokens - 1
    redis.call("HSET", KEYS[i], "tokens", states[i].tokens, "last_ms", now_ms)
    redis.call("EXPIRE", KEYS[i], states[i].ttl_s)
end

return 0
`

// Spec is the stable, pre-encoded part of a token bucket. Construct specs once
// and pair them with a key per request; this keeps float and TTL formatting off
// the message path.
type Spec struct {
	capacityArg string
	refillArg   string
	ttlArg      string
}

// NewSpec prepares a token-bucket configuration. Invalid configurations panic
// because every caller builds process constants during initialization.
func NewSpec(capacity, refillPerSecond float64) Spec {
	if capacity <= 0 || refillPerSecond <= 0 {
		panic("ratelimit: capacity and refill rate must be positive")
	}
	ttl := int64(math.Ceil(capacity/refillPerSecond)) * 2
	return Spec{
		capacityArg: strconv.FormatFloat(capacity, 'f', -1, 64),
		refillArg:   strconv.FormatFloat(refillPerSecond/1000.0, 'g', -1, 64),
		ttlArg:      strconv.FormatInt(ttl, 10),
	}
}

// Request binds a prepared bucket configuration to its Valkey key.
type Request struct {
	Key  string
	Spec Spec
}

// ForKey binds a prepared spec to a key.
func (s Spec) ForKey(key string) Request { return Request{Key: key, Spec: s} }

type Limiter struct {
	client valkey.Client
	script *valkey.Lua
}

func New(client valkey.Client) *Limiter {
	return &Limiter{
		client: client,
		// Deliberately not NewLuaScriptRetryable: after a connection failure the
		// mutation outcome is ambiguous, and an automatic retry could double-spend.
		script: valkey.NewLuaScript(luaOrderedTokenBucket),
	}
}

var errEmptyKey = errors.New("ratelimit: empty bucket key")

// Allow consumes one token from one bucket and reports whether it was available.
func (l *Limiter) Allow(ctx context.Context, req Request) (bool, error) {
	denied, err := l.allow(ctx, req, nil)
	if err != nil {
		return false, err
	}
	return denied == 0, nil
}

// AllowOrdered consumes first and then second in one atomic script execution.
// If first denies, second is untouched. If second denies, first remains consumed,
// matching the former pair of sequential calls. denied is zero on success, one
// for first, and two for second.
func (l *Limiter) AllowOrdered(ctx context.Context, first, second Request) (denied uint8, err error) {
	return l.allow(ctx, first, &second)
}

func (l *Limiter) allow(ctx context.Context, first Request, second *Request) (uint8, error) {
	if first.Key == "" || second != nil && second.Key == "" {
		return 0, errEmptyKey
	}

	keys := [2]string{first.Key}
	args := [6]string{first.Spec.capacityArg, first.Spec.refillArg, first.Spec.ttlArg}
	count := 1
	if second != nil {
		keys[1] = second.Key
		args[3] = second.Spec.capacityArg
		args[4] = second.Spec.refillArg
		args[5] = second.Spec.ttlArg
		count = 2
	}

	result, err := l.script.Exec(ctx, l.client, keys[:count], args[:count*3]).AsInt64()
	if err != nil {
		return 0, err
	}
	if result < 0 || result > int64(count) {
		return 0, errors.New("ratelimit: invalid script decision")
	}
	return uint8(result), nil
}
