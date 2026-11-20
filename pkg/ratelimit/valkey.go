// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package ratelimit

import (
	"context"
	"errors"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/valkey-io/valkey-go"
)

const luaOrderedTokenBucket = `
local count = #KEYS
if count < 1 or count > 2 then
    return redis.error_reply("token bucket requires one or two keys")
end
local report_retry = #ARGV == count * 4 + 1 and ARGV[#ARGV] == "retry"
if #ARGV ~= count * 4 and not report_retry then
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
    if report_retry then
        local refill_per_ms = tonumber(ARGV[(denied - 1) * 4 + 2])
        return {denied, math.ceil((1 - states[denied].tokens) / refill_per_ms)}
    end
    return denied
end

for i = 1, count do
    states[i].tokens = states[i].tokens - 1
    redis.call("HSET", KEYS[i], "tokens", states[i].tokens, "last_ms", now_ms)
    redis.call("EXPIRE", KEYS[i], states[i].ttl_s)
end

if report_retry then
    return {0, 0}
end
return 0
`

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
	profileHelixUser
)

func detectProfile(capacity int, refill float64) uint8 {
	switch capacity {
	case 20:
		return profileChat
	case 100:
		const chatModRefillFloor = 2.0
		if refill > chatModRefillFloor {
			return profileChatMod
		}
		return profileHelixSystem
	case 700:
		return profileHelixGeneral
	case 800:
		return profileHelixUser
	default:
		return profileUnknown
	}
}

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

func (s Spec) ForKey(key string) Request { return Request{Key: key, Spec: s} }

func (s Spec) ForDynamicKey(valkeyPrefix, scope, value string) Request {
	return Request{DynamicPrefix: valkeyPrefix, Bucket: BucketID{Scope: scope, Value: value}, Spec: s}
}

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
		// Not retryable: after a connection failure the outcome is ambiguous and a retry double-spends.
		script: valkey.NewLuaScript(luaOrderedTokenBucket),
	}
}

var errEmptyKey = errors.New("ratelimit: empty bucket key")

func (l *Limiter) Allow(ctx context.Context, req Request) (bool, error) {
	denied, err := l.allow(ctx, bucketCheck{first: req})
	if err != nil {
		return false, err
	}
	return denied == 0, nil
}

func (l *Limiter) AllowOrdered(ctx context.Context, first, second Request) (denied uint8, err error) {
	return l.allow(ctx, bucketCheck{first: first, second: &second})
}

func (l *Limiter) AllowOrderedWithRetry(ctx context.Context, first, second Request) (uint8, time.Duration, error) {
	res, err := l.runCheck(ctx, bucketCheck{first: first, second: &second, reportRetry: true})
	if err != nil {
		return 0, 0, err
	}
	decision, err := res.AsIntSlice()
	if err != nil {
		return 0, 0, err
	}
	if len(decision) != 2 {
		return 0, 0, errInvalidDecision
	}
	denied, err := bucketDecision(decision[0], 2)
	return denied, bucketRetryAfter(decision[1]), err
}

func bucketRetryAfter(ms int64) time.Duration {
	if ms < 0 || ms > math.MaxInt64/int64(time.Millisecond) {
		return 0
	}
	return time.Duration(ms) * time.Millisecond
}

func (l *Limiter) allow(ctx context.Context, check bucketCheck) (uint8, error) {
	res, err := l.runCheck(ctx, check)
	if err != nil {
		return 0, err
	}
	result, err := res.AsInt64()
	if err != nil {
		return 0, err
	}
	return bucketDecision(result, check.count())
}

func (l *Limiter) AllowEmergency(ctx context.Context, req Request, generation uint64, validFromMS int64) (bool, error) {
	emergency := Request{Key: emergencyKey(req.valkeyKey(), generation), Spec: req.Spec.emergency()}
	denied, err := l.allow(ctx, bucketCheck{first: emergency, originMS: validFromMS})
	return denied == 0, err
}

func (l *Limiter) AllowEmergencyOrdered(ctx context.Context, first, second Request, generation uint64, validFromMS int64) (uint8, error) {
	first = Request{Key: emergencyKey(first.valkeyKey(), generation), Spec: first.Spec.emergency()}
	second = Request{Key: emergencyKey(second.valkeyKey(), generation), Spec: second.Spec.emergency()}
	return l.allow(ctx, bucketCheck{first: first, second: &second, originMS: validFromMS})
}

func emergencyKey(key string, generation uint64) string {
	return generationKey(key, "emergency", generation)
}

func generationKey(key, class string, generation uint64) string {
	return key + ":" + class + ":g" + strconv.FormatUint(generation, 10)
}

type bucketCheck struct {
	first       Request
	second      *Request
	originMS    int64
	reportRetry bool
}

func (c bucketCheck) count() int {
	if c.second != nil {
		return 2
	}
	return 1
}

func (l *Limiter) runCheck(ctx context.Context, check bucketCheck) (valkey.ValkeyResult, error) {
	origin := originArg(check.originMS)
	keys := [2]string{check.first.valkeyKey()}
	first := check.first.Spec
	args := [9]string{first.capacityArg, first.refillArg, first.ttlArg, origin}
	if second := check.second; second != nil {
		keys[1] = second.valkeyKey()
		args[4] = second.Spec.capacityArg
		args[5] = second.Spec.refillArg
		args[6] = second.Spec.ttlArg
		args[7] = origin
	}
	count := check.count()
	if keys[0] == "" || keys[count-1] == "" {
		return valkey.ValkeyResult{}, errEmptyKey
	}
	argv := args[:count*4]
	if check.reportRetry {
		argv = append(argv, "retry")
	}
	return l.script.Exec(ctx, l.client, keys[:count], argv), nil
}

var errInvalidDecision = errors.New("ratelimit: invalid script decision")

func bucketDecision(result int64, count int) (uint8, error) {
	if result < 0 || result > int64(count) {
		return 0, errInvalidDecision
	}
	return uint8(result), nil
}

func originArg(originMS int64) string {
	if originMS == 0 {
		return "0"
	}
	return strconv.FormatInt(originMS, 10)
}
