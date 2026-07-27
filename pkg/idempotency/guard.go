package idempotency

import (
	"time"

	"ItsBagelBot/pkg/bus"

	"go.uber.org/zap"
)

// Handler is the bus consume-handler shape Guard wraps: the same signature the
// weighted consumer hands each delivered message.
type Handler func(*bus.Message) error

// KeyFunc extracts the idempotency key from a message and reports whether the
// message is guardable. Returning ok=false passes the message straight through
// untouched, so the guard never invents a key that would wrongly collapse two
// distinct events. The strategy is injected rather than hard-coded because the
// stable identity of an event is a domain question (a payload field, a header),
// which this transport-level package must not decide on its own.
type KeyFunc func(*bus.Message) (string, bool)

// Metrics receives guard observations. Both methods must be safe for concurrent
// use; a nil Metrics is tolerated.
type Metrics interface {
	// Duplicate is called when a delivery was recognised as a replay and skipped.
	Duplicate()
	// FailOpen is called when the store errored and the guard admitted the event.
	FailOpen()
}

type nopMetrics struct{}

func (nopMetrics) Duplicate() {}
func (nopMetrics) FailOpen()  {}

// MessageUUIDKey keys on the transport identity bus already resolved (the
// Bagelbot-Message-Id header, the JetStream stream sequence, or a nuid). It is
// stable across a JetStream redelivery, and — once the flow-consumer retry path
// carries Bagelbot-Message-Id across the schedule hop — across a retry too.
func MessageUUIDKey(m *bus.Message) (string, bool) {
	if m == nil || m.UUID == "" {
		return "", false
	}
	return m.UUID, true
}

// Guard wraps a consume handler so a duplicate delivery of the same key does not
// re-run it. The claim lifecycle is CLAIM-BEFORE, RELEASE-ON-ERROR:
//
//   - Seen is one atomic SET NX, so two copies of the same event racing on one
//     pod cannot both pass — exactly one wins the claim and the other is skipped.
//     A "check then run then claim" order would let both run before either
//     claimed. The claim must therefore come before the handler.
//   - If the handler then returns an error the claim is released, so a message
//     that FAILED to process is never permanently suppressed: its redelivery or
//     retry re-claims and re-runs. (A hard process crash between claim and error
//     leaves the claim until its ttl; that window is acceptable for the
//     loss-tolerant effects this guards and bounded by the short ttl.)
//   - A store error fails open: the handler runs, so a backend outage never
//     drops a live message.
//
// A duplicate is treated as success (nil), so the caller acks/drops it and the
// effect is not re-run.
func Guard(store Store, key KeyFunc, ttl time.Duration, log *zap.Logger, metrics Metrics) func(Handler) Handler {
	if log == nil {
		log = zap.NewNop()
	}
	if metrics == nil {
		metrics = nopMetrics{}
	}
	if key == nil {
		key = MessageUUIDKey
	}
	return func(next Handler) Handler {
		return func(m *bus.Message) error {
			return guardOne(store, key, ttl, log, metrics, next, m)
		}
	}
}

func guardOne(store Store, key KeyFunc, ttl time.Duration, log *zap.Logger, metrics Metrics, next Handler, m *bus.Message) error {
	k, ok := key(m)
	if !ok {
		return next(m) // no stable identity: never guess one
	}
	ctx := m.Context()
	seen, err := store.Seen(ctx, k, ttl)
	if err != nil {
		metrics.FailOpen()
		log.Warn("idempotency guard failing open", zap.String("key", k), zap.Error(err))
		return next(m)
	}
	if seen {
		metrics.Duplicate()
		return nil // duplicate: skip the handler, ack as success
	}
	if herr := next(m); herr != nil {
		_ = store.Release(ctx, k)
		return herr
	}
	return nil
}
