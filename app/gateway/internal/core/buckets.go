package core

import (
	"context"
	"math"

	"ItsBagelBot/pkg/ratelimit"
)

// Buckets is one upstream API budget under the fleet's premium/standard lane
// discipline: standard traffic keeps 75% of the capacity so premium always has
// a reserve — the same split sesame's consumer pool applies to its routines.
// Every provider owns one Buckets per upstream key it spends.
type Buckets struct {
	key      string
	general  ratelimit.Spec
	standard ratelimit.Spec
}

// strictBucket derives a token bucket that can never exceed limit requests in
// ANY window of windowSeconds. A bucket with burst B and refill r admits up to
// B + r*window requests per window in the worst case (full burst, then a full
// window of refill), so sizing burst = refill-per-window = limit/2 keeps every
// rolling window at or under the upstream's allowance — a bucket naively sized
// capacity=limit, refill=limit/window would admit up to 2x the allowance.
// Burst is floored because ratelimit.NewSpec requires an integer capacity and
// panicking at boot over an odd configured limit would crashloop the pod.
func strictBucket(limit, windowSeconds float64) (burst, refillPerSecond float64) {
	burst = math.Max(1, math.Floor(limit/2))
	refillPerSecond = (limit - burst) / windowSeconds
	if refillPerSecond <= 0 {
		// Degenerate budget (limit ~1): NewSpec requires a positive refill;
		// strictness yields to a valid spec on a config that tiny.
		refillPerSecond = burst / windowSeconds
	}
	return burst, refillPerSecond
}

// NewBuckets derives the (general, standard) token-bucket pair for one
// upstream allowing capacity requests per windowSeconds — strictly: the
// general bucket bounds TOTAL spend to the allowance in every rolling window,
// and the standard bucket bounds standard-lane spend to 75% of it, so premium
// always keeps a 25% reserve.
func NewBuckets(key string, capacity, windowSeconds float64) Buckets {
	gen := math.Max(1, math.Floor(capacity))
	std := math.Max(1, math.Floor(gen*0.75))
	genBurst, genRefill := strictBucket(gen, windowSeconds)
	stdBurst, stdRefill := strictBucket(std, windowSeconds)
	return Buckets{
		key:      key,
		general:  ratelimit.NewSpec(genBurst, genRefill),
		standard: ratelimit.NewSpec(stdBurst, stdRefill),
	}
}

// WithKey returns a copy of b spending the same derived specs under a
// different limiter key, for per-caller budgets (govee: one bucket per
// broadcaster) without re-deriving the specs on every request.
func (b Buckets) WithKey(key string) Buckets {
	b.key = key
	return b
}

// Enforce consumes one request from the budget. Standard requests must pass
// both their restricted bucket AND the general bucket in one atomic script (a
// denial consumes neither); premium requests consume only the general bucket,
// enjoying the 25% reserve. A denial is a typed 429 UpstreamError so the
// provider's friendly mapping chats it back, and it is deliberately NOT
// negative-cacheable — the next request must retry the bucket.
func (b Buckets) Enforce(ctx context.Context, limiter *ratelimit.Limiter, isPremium bool) error {
	if limiter == nil {
		return nil
	}
	generalReq := ratelimit.Request{Key: b.key, Spec: b.general}

	if isPremium {
		ok, err := limiter.Allow(ctx, generalReq)
		if err != nil {
			return err
		}
		if !ok {
			return &UpstreamError{Status: 429, Message: "premium rate limit exceeded"}
		}
		return nil
	}

	standardReq := ratelimit.Request{Key: b.key + ":standard", Spec: b.standard}
	deniedIdx, err := limiter.AllowOrdered(ctx, standardReq, generalReq)
	if err != nil {
		return err
	}
	if deniedIdx != 0 {
		return &UpstreamError{Status: 429, Message: "standard rate limit exceeded"}
	}
	return nil
}
