// Package engine is the gossip service's runtime, mirroring sesame's module/engine
// split: providers (app/gossip/internal/providers) declare what they answer,
// the engine indexes and serves them. It owns the NATS subscription loop, the
// per-request orchestration (sonic decode, timeout, New Relic transaction,
// respond, slow-call logging) and the hot-path byte discipline: a handler that
// answers with pre-marshaled bytes (a cache hit) is responded verbatim, with
// no re-encode.
package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"ItsBagelBot/app/gossip/internal/provider"
	gossiprpc "ItsBagelBot/internal/domain/rpc/gossip"
	"ItsBagelBot/pkg/bus"
	"ItsBagelBot/pkg/monitor"

	"github.com/bytedance/sonic"
	"github.com/nats-io/nats.go"
	"github.com/newrelic/go-agent/v3/newrelic"
	"go.uber.org/zap"
)

const defaultTimeout = 5 * time.Second

// badRequestReply is the fixed reply for an undecodable request body.
var badRequestReply = []byte(`{"error":"bad request"}`)

// handlerPolicy is the concurrency budget each endpoint gets. It is PER
// SUBJECT, one fleet per endpoint, and that is the point: a subject can only
// ever starve itself. govee.control spends a 12s budget on two sequential 6s
// HTTP calls, and a shared fleet would let four of those park a fortnite.stats
// cache hit that answers in microseconds. Per-subject fleets keep the slow
// endpoint's misery local, which is the property the serial callbacks had and
// the only one worth keeping from them.
//
// Four is deliberately modest, and sized against memory rather than CPU — see
// bus.RPCPoolPolicy's defaults for the arithmetic against gossip's
// GOMEMLIMIT=96MiB, its 128Mi limit, and the 4MiB per-response buffer in
// app/gossip/internal/core/http.go. The bug being fixed is head-of-line
// blocking behind one slow handler, and four removes it.
var handlerPolicy = bus.RPCPoolPolicy{MinWorkers: 1, MaxWorkers: 4, QueueDepth: 4}

// Serve subscribes every endpoint of every provider at
// "<prefix>.<provider>.<endpoint>" in queueGroup, so replicas share the load.
// It flushes once after all subscriptions so a deploy never answers a subject
// list it has not fully registered.
//
// Each endpoint is served by its own handler pool (handlerPolicy), so HANDLERS
// RUN CONCURRENTLY WITH THEMSELVES: one endpoint answers up to four requests at
// a time instead of holding them behind whichever one is fetching. A provider
// handler that closes over mutable state must guard it; the byte-flow handlers
// the builder assembles only read the cache and the upstream, and the bespoke
// ones (govee, sessions) hold no cross-request state. The pools register
// themselves with bus.DrainRPCHandlers, which main calls on shutdown before the
// NATS and Valkey connections close.
func Serve(nc *nats.Conn, prefix, queueGroup string, providers []provider.Provider, nrApp *newrelic.Application, log *zap.Logger) error {
	for _, p := range providers {
		for _, ep := range p.Endpoints() {
			subject := gossiprpc.Subject(prefix, p.Name(), ep.Name)
			if err := subscribe(nc, subject, queueGroup, ep, nrApp, log); err != nil {
				return err
			}
			log.Debug("gossip endpoint registered", zap.String("subject", subject))
		}
	}
	if err := nc.Flush(); err != nil {
		return fmt.Errorf("flush subscriptions: %w", err)
	}
	return nil
}

// subscribe registers one endpoint handler on its own pool.
//
// Everything below runs on a pool worker, including the New Relic transaction.
// That is a requirement, not a convenience: a newrelic.Transaction has goroutine
// affinity, so starting one on the delivery goroutine and ending it on a worker
// would corrupt exactly the trace this instrumentation exists to produce. The
// delivery goroutine touches nothing but the *nats.Msg.
func subscribe(nc *nats.Conn, subject, queueGroup string, ep provider.Endpoint, nrApp *newrelic.Application, log *zap.Logger) error {
	timeout := ep.Timeout
	if timeout <= 0 {
		timeout = defaultTimeout
	}
	handle := ep.Handle

	registration := bus.RPCSubscription{Subject: subject, QueueGroup: queueGroup, Policy: handlerPolicy}
	_, err := bus.QueueSubscribeRPCConcurrent(nc, registration, func(msg *nats.Msg) {
		start := time.Now()

		txn := nrApp.StartTransaction("rpc " + subject)
		defer txn.End()
		log := monitor.TraceLogger(txn, log)

		// Empty bodies are allowed for no-argument RPCs; handlers validate any
		// required fields on the zero-value request.
		var req gossiprpc.Request
		if len(msg.Data) > 0 {
			if err := sonic.Unmarshal(msg.Data, &req); err != nil {
				txn.NoticeError(err)
				respondAndLog(msg, subject, start, log, badRequestReply)
				return
			}
		}

		ctx, cancel := context.WithTimeout(newrelic.NewContext(context.Background(), txn), timeout)
		defer cancel()

		respondAndLog(msg, subject, start, log, encode(subject, handle(ctx, req), log))
	})
	if err != nil {
		return fmt.Errorf("subscribe %s: %w", subject, err)
	}
	return nil
}

// encode renders one handler result for the wire. Pre-marshaled bytes (a
// json.RawMessage from the byte-flow cache) pass through untouched — that is
// the zero-work hit path; anything else is marshaled once with sonic.
func encode(subject string, result any, log *zap.Logger) []byte {
	switch v := result.(type) {
	case json.RawMessage:
		return v
	case []byte:
		return v
	default:
		b, err := sonic.Marshal(v)
		if err != nil {
			log.Error("gossip reply marshal failed", zap.String("subject", subject), zap.Error(err))
			return []byte(`{"error":"internal error"}`)
		}
		return b
	}
}

// respondAndLog answers the request and mirrors pkg/bus's slow-call logging so
// the gossip service's latency shows up the same way in kubectl as every other
// service's RPC surface.
func respondAndLog(msg *nats.Msg, subject string, start time.Time, log *zap.Logger, body []byte) {
	elapsed := time.Since(start)
	if err := msg.Respond(body); err != nil {
		log.Warn("rpc respond failed", zap.String("subject", subject), zap.Duration("elapsed", elapsed), zap.Error(err))
		return
	}
	if elapsed > 250*time.Millisecond {
		log.Debug("slow rpc handler", zap.String("subject", subject), zap.Duration("elapsed", elapsed))
	}
}
