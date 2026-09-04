// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package config

import "ItsBagelBot/pkg/env"

// Config is the process env app/discord/ingress boots from. There is no
// ROLE here: ingress is always the gateway Identify session now, one
// replica per bot token (see main.go).
type Config struct {
	ListenAddr      string
	ValkeyAddr      string
	ValkeyPassword  string
	DiscordBotToken string

	// NATSURL is where ingress publishes discord.ingress.event.* -- a plain
	// publisher, no durable consumer, since ingress reads nothing off NATS.
	NATSURL string
}

// Load reads process env. Empty DISCORD_BOT_TOKEN leaves the service idle
// with health still serving (see main.go): no token means no gateway
// Identify and nothing to relay.
func Load() Config {
	return Config{
		ListenAddr:      env.Get("LISTEN_ADDR", ":8080"),
		ValkeyAddr:      env.Get("VALKEY_ADDR", "127.0.0.1:6379"),
		ValkeyPassword:  env.Get("VALKEY_PASSWORD", ""),
		DiscordBotToken: env.Get("DISCORD_BOT_TOKEN", ""),
		NATSURL:         env.Get("NATS_URL", "nats://127.0.0.1:4222"),
	}
}
