// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package svcboot

import "ItsBagelBot/pkg/env"

type Infra struct {
	NATSURL    string
	NATSRPCURL string

	ValkeyAddr     string
	ValkeyPassword string

	ListenAddr string
}

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
