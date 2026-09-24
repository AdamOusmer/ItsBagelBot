// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package core

import (
	"context"
	"math"

	"ItsBagelBot/pkg/ratelimit"
)

type Buckets struct {
	key      string
	general  ratelimit.Spec
	standard ratelimit.Spec
}

// burst + refill*window must equal limit; capacity=limit with refill=limit/window admits 2x the quota.
func pacedBucket(limit, windowSeconds, maxBurst float64) (burst, refillPerSecond float64) {
	burst = math.Max(1, math.Min(maxBurst, math.Floor(limit/2)))
	refillPerSecond = (limit - burst) / windowSeconds
	if refillPerSecond <= 0 {
		refillPerSecond = burst / windowSeconds
	}
	return burst, refillPerSecond
}

func strictBucket(limit, windowSeconds float64) (burst, refillPerSecond float64) {
	return pacedBucket(limit, windowSeconds, math.Floor(limit/2))
}

func NewBuckets(key string, capacity, windowSeconds float64) Buckets {
	return NewPacedBuckets(key, capacity, windowSeconds, math.Floor(capacity/2))
}

func NewPacedBuckets(key string, capacity, windowSeconds, maxBurst float64) Buckets {
	gen := math.Max(1, math.Floor(capacity))
	std := math.Max(1, math.Floor(gen*0.75))
	genBurst, genRefill := pacedBucket(gen, windowSeconds, maxBurst)
	stdBurst, stdRefill := pacedBucket(std, windowSeconds, math.Max(1, math.Floor(maxBurst*0.75)))
	return Buckets{
		key:      key,
		general:  ratelimit.NewSpec(genBurst, genRefill),
		standard: ratelimit.NewSpec(stdBurst, stdRefill),
	}
}

func (b Buckets) WithKey(key string) Buckets {
	b.key = key
	return b
}

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
			return &UpstreamError{Status: 429, Message: "premium rate limit exceeded", LocalDeny: true}
		}
		return nil
	}

	standardReq := ratelimit.Request{Key: b.key + ":standard", Spec: b.standard}
	deniedIdx, err := limiter.AllowOrdered(ctx, standardReq, generalReq)
	if err != nil {
		return err
	}
	if deniedIdx != 0 {
		return &UpstreamError{Status: 429, Message: "standard rate limit exceeded", LocalDeny: true}
	}
	return nil
}
