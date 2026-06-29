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
	"strings"

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
// ARGV: capacity, refill-per-millisecond, TTL-seconds, empty-origin-ms for each
// key. Origin zero gives legacy/full initialization. A positive origin starts a
// new lease generation empty and reconstructs only refill earned since then.
const luaOrderedTokenBucket = `
local count = #KEYS
if count < 1 or count > 2 then
    return redis.error_reply("token bucket requires one or two keys")
end
if #ARGV ~= count * 4 then
    return redis.error_reply("invalid token bucket arguments")
end

local server_time = redis.call("TIME")
local now_ms = (tonumber(server_time[1]) * 1000) + math.floor(tonumber(server_time[2]) / 1000)
local states = {}

-- Read and validate every key before writing either key. redis.pcall turns a
-- WRONGTYPE into a value we can return before any state has changed.
for i = 1, count do
    local offset = ((i - 1) * 4)
    local capacity = tonumber(ARGV[offset + 1])
    local refill_per_ms = tonumber(ARGV[offset + 2])
    local ttl_s = tonumber(ARGV[offset + 3])
    local empty_origin_ms = tonumber(ARGV[offset + 4])
    if not capacity or capacity <= 0 or not refill_per_ms or refill_per_ms <= 0 or not ttl_s or ttl_s <= 0 or not empty_origin_ms or empty_origin_ms < 0 then
        return redis.error_reply("invalid token bucket spec")
    end

    local bucket = redis.pcall("HMGET", KEYS[i], "tokens", "last_ms")
    if bucket.err then
        return bucket
    end

    local tokens = tonumber(bucket[1])
    local last_ms = tonumber(bucket[2])
    if not tokens or not last_ms then
        if empty_origin_ms == 0 then
            tokens = capacity
        else
            tokens = math.min(capacity, math.max(0, now_ms - empty_origin_ms) * refill_per_ms)
        end
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

	capacity       int
	refillPerSec   float64
	emergencyArgs  [3]string
	emergencyBurst int
	emergencyRate  float64
	profile        uint8
}

// NewSpec prepares a token-bucket configuration. Invalid configurations panic
// because every caller builds process constants during initialization.
func NewSpec(capacity, refillPerSecond float64) Spec {
	if capacity <= 0 || refillPerSecond <= 0 || capacity != math.Trunc(capacity) {
		panic("ratelimit: capacity and refill rate must be positive")
	}
	ttl := int64(math.Ceil(capacity/refillPerSecond)) * 2
	emergencyBurst := int(math.Floor(capacity * 0.10))
	if emergencyBurst < 1 {
		emergencyBurst = 1
	}
	if emergencyBurst > int(capacity) {
		emergencyBurst = int(capacity)
	}
	emergencyRate := refillPerSecond * (float64(emergencyBurst) / capacity)
	emergencyTTL := int64(math.Ceil(float64(emergencyBurst)/emergencyRate)) * 2
	spec := Spec{
		capacityArg:  strconv.FormatFloat(capacity, 'f', -1, 64),
		refillArg:    strconv.FormatFloat(refillPerSecond/1000.0, 'g', -1, 64),
		ttlArg:       strconv.FormatInt(ttl, 10),
		capacity:     int(capacity),
		refillPerSec: refillPerSecond,
		emergencyArgs: [3]string{
			strconv.Itoa(emergencyBurst),
			strconv.FormatFloat(emergencyRate/1000.0, 'g', -1, 64),
			strconv.FormatInt(emergencyTTL, 10),
		},
		emergencyBurst: emergencyBurst,
		emergencyRate:  emergencyRate,
	}
	spec.profile = detectProfile(spec.capacity, spec.refillPerSec)
	return spec
}

func (s Spec) emergency() Spec {
	return Spec{
		capacityArg:  s.emergencyArgs[0],
		refillArg:    s.emergencyArgs[1],
		ttlArg:       s.emergencyArgs[2],
		capacity:     s.emergencyBurst,
		refillPerSec: s.emergencyRate,
		profile:      s.profile,
	}
}

const (
	profileUnknown uint8 = iota
	profileChat
	profileChatMod
	profileHelixGeneral
	profileHelixSystem
)

func detectProfile(capacity int, refill float64) uint8 {
	switch capacity {
	case 20:
		return profileChat
	case 100:
		// Chat-mod and Helix-system share a burst but not a refill window.
		if refill > 2 {
			return profileChatMod
		}
		return profileHelixSystem
	case 700:
		return profileHelixGeneral
	default:
		return profileUnknown
	}
}

// Request binds a prepared bucket configuration to its Valkey key.
type Request struct {
	Key           string
	DynamicPrefix string
	Bucket        BucketID
	Spec          Spec
}

type BucketID struct {
	Scope string `json:"scope"`
	Value string `json:"value,omitempty"`
}

// ForKey binds a prepared spec to a key.
func (s Spec) ForKey(key string) Request { return Request{Key: key, Spec: s} }

// ForDynamicKey defers string concatenation until Valkey is actually used.
// Local leased admission hashes the comparable BucketID directly.
func (s Spec) ForDynamicKey(valkeyPrefix, scope, value string) Request {
	return Request{DynamicPrefix: valkeyPrefix, Bucket: BucketID{Scope: scope, Value: value}, Spec: s}
}

// Pointer receivers: Request embeds Spec, so a value receiver would copy ~200
// bytes on every hot-path call.
func (r *Request) bucketID() BucketID {
	if r.Bucket.Scope != "" {
		return r.Bucket
	}
	return BucketID{Scope: strings.TrimPrefix(r.Key, "ratelimit:")}
}

func (r *Request) valkeyKey() string {
	if r.Key != "" {
		return r.Key
	}
	return r.DynamicPrefix + r.Bucket.Value
}

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

// AllowOrdered consumes both buckets in one atomic script execution. A denial
// consumes neither bucket. denied is zero on success, one for first, and two for
// second.
func (l *Limiter) AllowOrdered(ctx context.Context, first, second Request) (denied uint8, err error) {
	return l.allow(ctx, first, &second)
}

func (l *Limiter) allow(ctx context.Context, first Request, second *Request) (uint8, error) {
	return l.allowAt(ctx, first, second, 0, 0)
}

func (l *Limiter) AllowEmergency(ctx context.Context, req Request, generation uint64, validFromMS int64) (bool, error) {
	emergency := Request{Key: emergencyKey(req.valkeyKey(), generation), Spec: req.Spec.emergency()}
	denied, err := l.allowAt(ctx, emergency, nil, validFromMS, validFromMS)
	return denied == 0, err
}

func (l *Limiter) AllowEmergencyOrdered(ctx context.Context, first, second Request, generation uint64, validFromMS int64) (uint8, error) {
	first = Request{Key: emergencyKey(first.valkeyKey(), generation), Spec: first.Spec.emergency()}
	second = Request{Key: emergencyKey(second.valkeyKey(), generation), Spec: second.Spec.emergency()}
	return l.allowAt(ctx, first, &second, validFromMS, validFromMS)
}

func emergencyKey(key string, generation uint64) string {
	return generationKey(key, "emergency", generation)
}

func generationKey(key, class string, generation uint64) string {
	return key + ":" + class + ":g" + strconv.FormatUint(generation, 10)
}

func (l *Limiter) allowAt(ctx context.Context, first Request, second *Request, firstOriginMS, secondOriginMS int64) (uint8, error) {
	firstKey := first.valkeyKey()
	if firstKey == "" {
		return 0, errEmptyKey
	}
	secondKey := ""
	if second != nil {
		secondKey = second.valkeyKey()
		if secondKey == "" {
			return 0, errEmptyKey
		}
	}

	keys := [2]string{firstKey}
	args := [8]string{first.Spec.capacityArg, first.Spec.refillArg, first.Spec.ttlArg, originArg(firstOriginMS)}
	count := 1
	if second != nil {
		keys[1] = secondKey
		args[4] = second.Spec.capacityArg
		args[5] = second.Spec.refillArg
		args[6] = second.Spec.ttlArg
		args[7] = originArg(secondOriginMS)
		count = 2
	}

	result, err := l.script.Exec(ctx, l.client, keys[:count], args[:count*4]).AsInt64()
	if err != nil {
		return 0, err
	}
	if result < 0 || result > int64(count) {
		return 0, errors.New("ratelimit: invalid script decision")
	}
	return uint8(result), nil
}

func originArg(originMS int64) string {
	if originMS == 0 {
		return "0"
	}
	return strconv.FormatInt(originMS, 10)
}
