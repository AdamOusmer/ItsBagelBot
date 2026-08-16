// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary and unlicensed. See LICENSE.md.

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

// Config is what Guard needs to wrap a handler. It is a struct rather than a
// positional argument list because three of the five are optional and defaulted:
// as arguments, a caller that wanted to override only the last one still had to
// spell every slot before it, in an order where two adjacent nils are
// interchangeable to the compiler and are not interchangeable at run time.
type Config struct {
	// Store is the atomic claim primitive. It has no default: a guard with no
	// store is a guard that cannot claim.
	Store Store
	// Key defaults to MessageUUIDKey.
	Key KeyFunc
	// TTL bounds one claim. It is also the window a claim left behind by a hard
	// crash between claim and error survives for; see Guard.
	TTL time.Duration
	// Log defaults to a no-op logger, Metrics to a no-op sink.
	Log     *zap.Logger
	Metrics Metrics
}

// withDefaults resolves the optional dependencies once, at wrap time, so the
// per-delivery path never re-tests them.
func (cfg Config) withDefaults() Config {
	if cfg.Log == nil {
		cfg.Log = zap.NewNop()
	}
	if cfg.Metrics == nil {
		cfg.Metrics = nopMetrics{}
	}
	if cfg.Key == nil {
		cfg.Key = MessageUUIDKey
	}
	return cfg
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
func Guard(cfg Config) func(Handler) Handler {
	guard := cfg.withDefaults()
	return func(next Handler) Handler {
		return func(m *bus.Message) error {
			return guard.guardOne(next, m)
		}
	}
}

func (cfg *Config) guardOne(next Handler, m *bus.Message) error {
	k, ok := cfg.Key(m)
	if !ok {
		return next(m) // no stable identity: never guess one
	}
	ctx := m.Context()
	seen, err := cfg.Store.Seen(ctx, k, cfg.TTL)
	if err != nil {
		cfg.Metrics.FailOpen()
		cfg.Log.Warn("idempotency guard failing open", zap.String("key", k), zap.Error(err))
		return next(m)
	}
	if seen {
		cfg.Metrics.Duplicate()
		return nil // duplicate: skip the handler, ack as success
	}
	if herr := next(m); herr != nil {
		_ = cfg.Store.Release(ctx, k)
		return herr
	}
	return nil
}
