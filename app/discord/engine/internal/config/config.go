// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package config

import "ItsBagelBot/pkg/env"

// Config is the process env app/discord/engine boots from.
type Config struct {
	ListenAddr     string
	ValkeyAddr     string
	ValkeyPassword string

	// NATSURL is where engine binds its durable consumers: the six
	// discord.ingress.event.* subjects, plus the Twitch stream/clip subjects
	// Live/Clip consume (see modules/live.go, modules/clip.go).
	NATSURL string
	// NATSRPCURL is engine's outbound RPC connection: calls into
	// app/discord/outgress's internal channel/live RPC, and Twitch
	// outgress's streaminfo RPC.
	NATSRPCURL string

	// DiscordOutgressRPCPrefix addresses app/discord/outgress's internal
	// channel-management/live RPC (see internal/domain/rpc/discordoutgress).
	// This is NOT bagel.rpc.dingress -- that prefix stays on the
	// dashboard-facing guild setup/layout/unbind RPC, ported unchanged into
	// app/discord/outgress; this is a private prefix between engine and
	// outgress only.
	DiscordOutgressRPCPrefix string
	// TwitchOutgressRPCPrefix is Twitch outgress's (app/outgress) own RPC
	// prefix -- engine is a CALLER here (the go-live embed's Helix-details
	// fallback), the same relationship dingress's egress role had to it.
	TwitchOutgressRPCPrefix string

	// StreamLaneSubject/ClipCreatedSubject are the Twitch inputs Live/Clip
	// bind their own durable consumers to, same env names outgress and the
	// old dingress egress role already use so ops set one knob per subject.
	StreamLaneSubject  string
	ClipCreatedSubject string
}

// Load reads process env.
func Load() Config {
	natsURL := env.Get("NATS_URL", "nats://127.0.0.1:4222")
	return Config{
		ListenAddr:               env.Get("LISTEN_ADDR", ":8080"),
		ValkeyAddr:               env.Get("VALKEY_ADDR", "127.0.0.1:6379"),
		ValkeyPassword:           env.Get("VALKEY_PASSWORD", ""),
		NATSURL:                  natsURL,
		NATSRPCURL:               env.Get("NATS_RPC_URL", natsURL),
		DiscordOutgressRPCPrefix: env.Get("NATS_DISCORD_OUTGRESS_RPC_PREFIX", "bagel.rpc.discord-outgress"),
		TwitchOutgressRPCPrefix:  env.Get("NATS_OUTGRESS_RPC_PREFIX", "bagel.rpc.outgress"),
		StreamLaneSubject:        env.Get("NATS_SUBJECT_LANE_STREAM", "twitch.ingress.event.stream"),
		ClipCreatedSubject:       env.Get("NATS_SUBJECT_CLIP_CREATED", "data.twitch.clip.created"),
	}
}
