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

// NewBuckets derives the (general, standard) token-bucket pair for one
// upstream allowing capacity requests per windowSeconds. Capacities are
// floored because ratelimit.NewSpec requires integers and panicking at boot
// over an odd configured limit would crashloop the pod.
func NewBuckets(key string, capacity, windowSeconds float64) Buckets {
	gen := math.Max(1, math.Floor(capacity))
	std := math.Max(1, math.Floor(gen*0.75))
	return Buckets{
		key:      key,
		general:  ratelimit.NewSpec(gen, gen/windowSeconds),
		standard: ratelimit.NewSpec(std, std/windowSeconds),
	}
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
