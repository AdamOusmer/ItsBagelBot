// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package svcboot

import "ItsBagelBot/pkg/env"

// Infra is the endpoint block every fleet service reads from its environment
// under the same five names with the same defaults: the JetStream URL, the RPC
// URL derived from it, the Valkey address and password, and the health listen
// address.
//
// It is an embedded struct rather than five free functions because every
// service config already declared exactly these fields by these names: embedding
// keeps `cfg.NATSURL` and friends reading identically at ~200 call sites while
// making the defaults one decision instead of seven copies. Seven copies is not
// hypothetical drift — before this landed, app/twitch/outgress had no ListenAddr
// field at all and read LISTEN_ADDR inline at its one use, so the service's
// health port was invisible in its config.
//
// NATSRPCURL defaults to NATSURL rather than to a literal: the split exists so
// the RPC plane can be pointed at a different account or leaf node, and a
// deployment that has not split them must not have to set both.
type Infra struct {
	// NATSURL is the JetStream plane: publishers and durable consumers.
	NATSURL string
	// NATSRPCURL is the core request/reply plane, a separate connection pool.
	NATSRPCURL string

	ValkeyAddr     string
	ValkeyPassword string

	// ListenAddr is where pkg/health serves /healthz, /readyz, /drain and
	// /status.
	ListenAddr string
}

// LoadInfra reads the shared block. A service config embeds Infra and fills it
// with this in its own Load.
func LoadInfra() Infra {
	natsURL := env.Get("NATS_URL", "nats://127.0.0.1:4222")
	return Infra{
		NATSURL:        natsURL,
		NATSRPCURL:     env.Get("NATS_RPC_URL", natsURL),
		ValkeyAddr:     env.Get("VALKEY_ADDR", "127.0.0.1:6379"),
		ValkeyPassword: env.Get("VALKEY_PASSWORD", ""),
		ListenAddr:     env.Get("LISTEN_ADDR", ":8080"),
	}
}
