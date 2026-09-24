// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package config

import (
	"ItsBagelBot/pkg/env"
	"ItsBagelBot/pkg/svcboot"
)

type Config struct {
	svcboot.Infra

	DiscordBotToken string
	// Must be the public counts RPC: this internet-facing pod must never reach an admin subject.
	UsersCountsSubject string
}

func Load() Config {
	return Config{
		Infra:              svcboot.LoadInfra(),
		DiscordBotToken:    env.Get("DISCORD_BOT_TOKEN", ""),
		UsersCountsSubject: env.Get("NATS_INTERNAL_USERS_COUNTS_SUBJECT", "bagel.rpc.internal.users.counts.get"),
	}
}
