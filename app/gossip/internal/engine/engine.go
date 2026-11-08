// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"context"
	"fmt"
	"time"

	"ItsBagelBot/app/gossip/internal/core"
	"ItsBagelBot/app/gossip/internal/provider"
	gossiprpc "ItsBagelBot/internal/domain/rpc/gossip"
	"ItsBagelBot/pkg/bus"
	"ItsBagelBot/pkg/codec"
	"ItsBagelBot/pkg/monitor"

	"github.com/nats-io/nats.go"
	"github.com/newrelic/go-agent/v3/newrelic"
	"go.uber.org/zap"
)

const defaultTimeout = 5 * time.Second

var badRequestReply = []byte(`{"error":"bad request"}`)

var handlerPolicy = bus.RPCPoolPolicy{MinWorkers: 1, MaxWorkers: 4, QueueDepth: 4}

// Handlers run concurrently with themselves; one closing over mutable state must guard it.
func Serve(wiring bus.RPCWiring, prefix string, providers []provider.Provider) error {
	wiring.Pool = handlerPolicy
	for _, p := range providers {
		for _, ep := range p.Endpoints() {
			subject := gossiprpc.Subject(prefix, p.Name(), ep.Name)
			endpoint := serverEndpoint{provider: p.Name(), subject: subject, spec: ep, log: wiring.Log}
			if err := endpoint.subscribe(wiring); err != nil {
				return err
			}
			wiring.Log.Debug("gossip endpoint registered", zap.String("subject", subject))
		}
	}
	if err := wiring.NC.Flush(); err != nil {
		return fmt.Errorf("flush subscriptions: %w", err)
	}
	return nil
}

type serverEndpoint struct {
	provider string
	subject  string
	spec     provider.Endpoint
	log      *zap.Logger
}

func (e serverEndpoint) subscribe(wiring bus.RPCWiring) error {
	timeout := e.spec.Timeout
	if timeout <= 0 {
		timeout = defaultTimeout
	}
	return bus.ServeMsg(wiring.Within(timeout), e.subject, e.handle)
}

func (e serverEndpoint) handle(ctx context.Context, msg *nats.Msg) {
	start := time.Now()
	txn := newrelic.FromContext(ctx)
	// Both names come from the provider registry, never from request fields.
	txn.SetName("gossip " + e.provider + "." + e.spec.Name)
	txn.AddAttribute("gossip.provider", e.provider)
	txn.AddAttribute("gossip.endpoint", e.spec.Name)
	ctx = core.WithRequestTelemetry(ctx, e.provider, e.spec.Name)
	log := monitor.TraceLogger(txn, e.log)

	var req gossiprpc.Request
	if len(msg.Data) > 0 {
		if err := codec.Unmarshal(msg.Data, &req); err != nil {
			txn.AddAttribute("gossip.outcome", "invalid")
			e.respondAndLog(msg, start, txn, badRequestReply)
			return
		}
	}

	fetchCtx, cancel := core.FetchContext(ctx)
	defer cancel()
	body := encode(e.subject, e.spec.Handle(fetchCtx, req), log)
	recordOutcome(txn, core.ReplyOutcome(fetchCtx, body))
	e.respondAndLog(msg, start, txn, body)
}

func recordOutcome(txn *newrelic.Transaction, outcome string) {
	txn.AddAttribute("gossip.outcome", outcome)
	switch outcome {
	case "timeout", "canceled", "upstream_error", "internal_error":
		// The bounded outcome is safe even when provider error text contains credentials or URLs.
		monitor.NoticeError(txn, fmt.Errorf("gossip %s", outcome))
	}
}

func encode(subject string, result any, log *zap.Logger) []byte {
	switch v := result.(type) {
	case codec.RawMessage:
		return v
	case []byte:
		return v
	default:
		b, err := codec.Marshal(v)
		if err != nil {
			log.Error("gossip reply marshal failed", zap.String("subject", subject), zap.Error(err))
			return []byte(`{"error":"internal error"}`)
		}
		return b
	}
}

func (e serverEndpoint) respondAndLog(msg *nats.Msg, start time.Time, txn *newrelic.Transaction, body []byte) {
	log := monitor.TraceLogger(txn, e.log)
	elapsed := time.Since(start)
	if err := msg.Respond(body); err != nil {
		txn.AddAttribute("gossip.reply", "error")
		monitor.NoticeError(txn, fmt.Errorf("gossip reply failed"))
		log.Warn("rpc respond failed", zap.String("subject", e.subject), zap.Duration("elapsed", elapsed), zap.Error(err))
		return
	}
	txn.AddAttribute("gossip.reply", "ok")
	if elapsed > 250*time.Millisecond {
		log.Debug("slow rpc handler", zap.String("subject", e.subject), zap.Duration("elapsed", elapsed))
	}
}
