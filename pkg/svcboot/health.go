// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package svcboot

import (
	"github.com/nats-io/nats.go"
	"go.uber.org/zap"

	"ItsBagelBot/pkg/bus"
	"ItsBagelBot/pkg/health"
)

type Health struct {
	Log        *zap.Logger
	NC         *nats.Conn
	Service    string
	QueueGroup string
	ListenAddr string
}

func NewHealthSet(h Health, checks ...health.Check) *health.Set {
	set := health.NewSet(h.Service, append([]health.Check{health.NATS("nats", h.NC)}, checks...)...)

	rpcHealth, err := bus.SubscribeRPCHealth(h.NC, h.Service, h.QueueGroup, set)
	FatalIf(h.Log, err, "failed to subscribe rpc health")
	set.Add(rpcHealth)

	return set
}

func ServeHealth(h Health, checks ...health.Check) {
	health.ServeSet(h.ListenAddr, NewHealthSet(h, checks...))
}
