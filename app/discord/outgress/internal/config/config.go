// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package config

import (
	"ItsBagelBot/internal/discordstore"
	"ItsBagelBot/pkg/env"
	"ItsBagelBot/pkg/svcboot"
)

// Config is the process env app/discord/outgress boots from. outgress holds
// every Discord REST call in the split (see main.go's package doc), so it
// is the one Discord service safe at any replica count -- no gateway
// Identify session, no in-process state a second replica could diverge on.
type Config struct {
	// Infra is the shared NATS/Valkey/listen block (see svcboot.Infra); its
	// fields are promoted, so cfg.NATSURL and cfg.ListenAddr read unchanged.
	svcboot.Infra

	DiscordBotToken string

	// RPCPrefix/RPCQueue address the dashboard-facing guild
	// setup/layout/unbind/post RPC, ported unchanged from
	// app/dingress/internal/egress/rpc.go. Kept as NATS_DINGRESS_RPC_PREFIX
	// (default bagel.rpc.dingress): the subject is wire-compatible with
	// every existing caller (the console) and renaming it is out of scope
	// here -- only the Go-level service name "dingress" is going away.
	RPCPrefix string
	RPCQueue  string

	// DiscordEngineRPCPrefix/Queue address the NEW internal RPC engine calls
	// for channel-lifecycle and go-live operations a Command cannot express
	// (see internal/domain/rpc/discordoutgress). Private to engine and
	// outgress; never touched by the dashboard.
	DiscordEngineRPCPrefix string
	DiscordEngineRPCQueue  string

	// OutgressRPCPrefix is TWITCH outgress's own RPC prefix (app/twitch/outgress),
	// unrelated to this service's own two prefixes above. Unused today (no
	// handler here calls it), kept only because deploy's existing env
	// wiring for the old dingress-egress role already sets it and a removed
	// var is a needless deploy-side edit for the agent that owns deploy/**.
	OutgressRPCPrefix string
	// DiscordDataRPCPrefix addresses app/db/discord, the MySQL-backed store
	// behind guild bindings and per-guild settings. See
	// internal/discordstore.NewRPC.
	DiscordDataRPCPrefix string
}

// Load reads process env. Empty DISCORD_BOT_TOKEN leaves the service idle
// with health still serving.
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
