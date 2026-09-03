// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package config

import "ItsBagelBot/pkg/env"

// Config is the process env dingress boots from.
type Config struct {
	ListenAddr      string
	ValkeyAddr      string
	ValkeyPassword  string
	DiscordBotToken string
}

// Load reads process env. Empty DISCORD_BOT_TOKEN leaves the gateway dark.
func Load() Config {
	return Config{
		ListenAddr:      env.Get("LISTEN_ADDR", ":8080"),
		ValkeyAddr:      env.Get("VALKEY_ADDR", "127.0.0.1:6379"),
		ValkeyPassword:  env.Get("VALKEY_PASSWORD", ""),
		DiscordBotToken: env.Get("DISCORD_BOT_TOKEN", ""),
	}
}
