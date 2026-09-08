// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package config

import (
	"ItsBagelBot/pkg/env"
	"ItsBagelBot/pkg/svcboot"
)

// Config is the process env app/discord/ingress boots from. There is no
// ROLE here: ingress is always the gateway Identify session now, one
// replica per bot token (see main.go).
type Config struct {
	// NATSURL is where ingress publishes discord.ingress.event.* -- a plain
	// publisher, no durable consumer, since ingress reads nothing off NATS.
	// NATSRPCURL is ingress's outbound RPC connection: the one call it makes
	// is the users service's counts.get, to build the gateway presence
	// status. Kept separate from NATSURL/the publisher (see
	// presence.NewFetch's doc) even though it defaults to the same address,
	// matching every other service's NATS_RPC_URL/NATS_URL split.
	// Infra is the shared NATS/Valkey/listen block (see svcboot.Infra); its
	// fields are promoted, so cfg.NATSURL and cfg.ListenAddr read unchanged.
	svcboot.Infra

	DiscordBotToken string
	// UsersCountsSubject is the users service's narrow public counts RPC
	// (see app/db/users/rpc/counts.go). Never point this at an admin subject.
	UsersCountsSubject string
}

// Load reads process env. Empty DISCORD_BOT_TOKEN leaves the service idle
// with health still serving (see main.go): no token means no gateway
// Identify and nothing to relay.
func Load() Config {
	return Config{
		Infra:              svcboot.LoadInfra(),
		DiscordBotToken:    env.Get("DISCORD_BOT_TOKEN", ""),
		UsersCountsSubject: env.Get("NATS_INTERNAL_USERS_COUNTS_SUBJECT", "bagel.rpc.internal.users.counts.get"),
	}
}
