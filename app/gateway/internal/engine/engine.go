// Package engine is the gateway's runtime, mirroring sesame's module/engine
// split: providers (app/gateway/internal/providers) declare what they answer,
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

	"ItsBagelBot/app/gateway/internal/provider"
	gatewayrpc "ItsBagelBot/internal/domain/rpc/gateway"
	"ItsBagelBot/pkg/bus"

	"github.com/bytedance/sonic"
	"github.com/nats-io/nats.go"
	"github.com/newrelic/go-agent/v3/newrelic"
	"go.uber.org/zap"
)

const defaultTimeout = 5 * time.Second

// badRequestReply is the fixed reply for an undecodable request body.
var badRequestReply = []byte(`{"error":"bad request"}`)

// Serve subscribes every endpoint of every provider at
// "<prefix>.<provider>.<endpoint>" in queueGroup, so replicas share the load.
// It flushes once after all subscriptions so a deploy never answers a subject
// list it has not fully registered.
func Serve(nc *nats.Conn, prefix, queueGroup string, providers []provider.Provider, nrApp *newrelic.Application, log *zap.Logger) error {
	for _, p := range providers {
		for _, ep := range p.Endpoints() {
			subject := gatewayrpc.Subject(prefix, p.Name(), ep.Name)
			if err := subscribe(nc, subject, queueGroup, ep, nrApp, log); err != nil {
				return err
			}
			log.Debug("gateway endpoint registered", zap.String("subject", subject))
		}
	}
	if err := nc.Flush(); err != nil {
		return fmt.Errorf("flush subscriptions: %w", err)
	}
	return nil
}

// subscribe registers one endpoint handler.
func subscribe(nc *nats.Conn, subject, queueGroup string, ep provider.Endpoint, nrApp *newrelic.Application, log *zap.Logger) error {
	timeout := ep.Timeout
	if timeout <= 0 {
		timeout = defaultTimeout
	}
	handle := ep.Handle

	err := bus.QueueSubscribeRPC(nc, subject, queueGroup, func(msg *nats.Msg) {
		start := time.Now()

		txn := nrApp.StartTransaction("rpc " + subject)
		defer txn.End()

		// Empty bodies are allowed for no-argument RPCs; handlers validate any
		// required fields on the zero-value request.
		var req gatewayrpc.Request
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
			log.Error("gateway reply marshal failed", zap.String("subject", subject), zap.Error(err))
			return []byte(`{"error":"internal error"}`)
		}
		return b
	}
}

// respondAndLog answers the request and mirrors pkg/bus's slow-call logging so
// the gateway's latency shows up the same way in kubectl as every other
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
