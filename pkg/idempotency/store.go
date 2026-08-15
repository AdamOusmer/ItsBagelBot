// Package idempotency is the fleet's consumer-side duplicate guard. A JetStream
// redelivery (a quorum-loss resend of the same stored message) and a
// schedule-retry hop (the flow consumer re-emits a failed event) both deliver
// the same logical event more than once, so a handler with a non-idempotent side
// effect — a counter increment, a currency accrual, a row insert — would apply
// it twice. This package claims a per-event key at most once and skips the
// wrapped effect on any later delivery of the same key.
//
// It is deliberately layered so the two concerns compose without coupling:
//
//   - Store is the atomic claim primitive: Seen(key) is one SET NX that both
//     tests and takes the claim in a single master round trip, and Release(key)
//     drops it so a failed effect is not permanently suppressed.
//   - ValkeyStore implements Store over the fleet Valkey client, pinned to the
//     Sentinel-elected primary (a claim tested against a lagging replica could
//     miss a just-written duplicate). It fails OPEN: a Valkey outage admits the
//     event rather than dropping it.
//   - NewTiered fronts any Store with a per-pod LRU claim cache, so a same-pod
//     redelivery burst short-circuits with zero network.
//   - Guard adapts a Store into bus consume-handler middleware.
//
// The package imports pkg/bus (Guard's handler shape) and pkg/valkey (the
// primary pin) one-directionally; neither imports this package, so there is no
// cycle.
package idempotency

import (
	"context"
	"time"
)

// Store claims one idempotency key at most once. Implementations must make the
// test-and-claim a single atomic operation: a split "read then write" would
// route the read to a possibly-lagging replica and let a just-claimed duplicate
// slip through.
type Store interface {
	// Seen atomically claims key for ttl and reports whether it was ALREADY
	// present — that is, whether this delivery is a duplicate. A false return
	// means this caller won a fresh claim and should run the effect. On a store
	// failure implementations fail OPEN (return false), so a backend outage
	// admits the event rather than dropping it.
	Seen(ctx context.Context, key string, ttl time.Duration) (bool, error)

	// Release drops a claim so a caller whose effect then failed does not block
	// the legitimate redelivery. Releasing an absent or expired key is a no-op.
	Release(ctx context.Context, key string) error
}
