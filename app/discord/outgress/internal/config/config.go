// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package config

import (
	"ItsBagelBot/internal/discordstore"
	"ItsBagelBot/pkg/env"
	"ItsBagelBot/pkg/svcboot"
)

type Config struct {
	svcboot.Infra

	DiscordBotToken string

	// The bagel.rpc.dingress default stays for wire compatibility with the console.
	RPCPrefix string
	RPCQueue  string

	DiscordEngineRPCPrefix string
	DiscordEngineRPCQueue  string

	OutgressRPCPrefix    string
	DiscordDataRPCPrefix string
}

func Load() Config {
	return Config{
		Infra:                  svcboot.LoadInfra(),
		DiscordBotToken:        env.Get("DISCORD_BOT_TOKEN", ""),
		RPCPrefix:              env.Get("NATS_DINGRESS_RPC_PREFIX", "bagel.rpc.dingress"),
		RPCQueue:               env.Get("NATS_DINGRESS_RPC_QUEUE", "dingress-rpc"),
		DiscordEngineRPCPrefix: env.Get("NATS_DISCORD_OUTGRESS_RPC_PREFIX", "bagel.rpc.discord-outgress"),
		DiscordEngineRPCQueue:  env.Get("NATS_DISCORD_OUTGRESS_RPC_QUEUE", "discord-outgress-rpc"),
		OutgressRPCPrefix:      env.Get("NATS_OUTGRESS_RPC_PREFIX", "bagel.rpc.outgress"),
		DiscordDataRPCPrefix:   env.Get("NATS_DISCORD_DATA_RPC_PREFIX", discordstore.DefaultRPCPrefix),
	}
}
