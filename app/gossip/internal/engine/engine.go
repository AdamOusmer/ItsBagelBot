// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"context"
	"fmt"
	"time"

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

		var req gossiprpc.Request
		if len(msg.Data) > 0 {
			if err := codec.Unmarshal(msg.Data, &req); err != nil {
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
