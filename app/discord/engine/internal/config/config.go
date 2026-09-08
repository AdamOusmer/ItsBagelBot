// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package config

import (
	"ItsBagelBot/internal/discordstore"
	"ItsBagelBot/pkg/env"
	"ItsBagelBot/pkg/svcboot"
)

// Config is the process env app/discord/engine boots from.
type Config struct {
	// NATSURL is where engine binds its durable consumers: the six
	// discord.ingress.event.* subjects, plus the Twitch stream/clip subjects
	// Live/Clip consume (see modules/live.go, modules/clip.go). NATSRPCURL is
	// engine's outbound RPC connection: calls into app/discord/outgress's
	// internal channel/live RPC, and Twitch outgress's streaminfo RPC.
	// Infra is the shared NATS/Valkey/listen block (see svcboot.Infra); its
	// fields are promoted, so cfg.NATSURL and cfg.ListenAddr read unchanged.
	svcboot.Infra

	// DiscordOutgressRPCPrefix addresses app/discord/outgress's internal
	// channel-management/live RPC (see internal/domain/rpc/discordoutgress).
	// This is NOT bagel.rpc.dingress -- that prefix stays on the
	// dashboard-facing guild setup/layout/unbind RPC, ported unchanged into
	// app/discord/outgress; this is a private prefix between engine and
	// outgress only.
	DiscordOutgressRPCPrefix string
	// TwitchOutgressRPCPrefix is Twitch outgress's (app/twitch/outgress) own RPC
	// prefix -- engine is a CALLER here (the go-live embed's Helix-details
	// fallback), the same relationship dingress's egress role had to it.
	TwitchOutgressRPCPrefix string
	// DiscordDataRPCPrefix addresses app/db/discord, the MySQL-backed store
	// behind guild bindings, per-guild settings, the ticket desk and member
	// XP. See internal/discordstore.NewRPC.
	DiscordDataRPCPrefix string

	// StreamLaneSubject/ClipCreatedSubject are the Twitch inputs Live/Clip
	// bind their own durable consumers to, same env names outgress and the
	// old dingress egress role already use so ops set one knob per subject.
	StreamLaneSubject  string
	ClipCreatedSubject string
	// UserChangedSubject is the account fact the identity module watches so a
	// tier upgrade or downgrade changes the bot's per-guild appearance at once
	// rather than at the next gateway reconnect.
	UserChangedSubject string
}

// Load reads process env.
func Load() Config {
	return Config{
		Infra:                    svcboot.LoadInfra(),
		DiscordOutgressRPCPrefix: env.Get("NATS_DISCORD_OUTGRESS_RPC_PREFIX", "bagel.rpc.discord-outgress"),
		TwitchOutgressRPCPrefix:  env.Get("NATS_OUTGRESS_RPC_PREFIX", "bagel.rpc.outgress"),
		DiscordDataRPCPrefix:     env.Get("NATS_DISCORD_DATA_RPC_PREFIX", discordstore.DefaultRPCPrefix),
		StreamLaneSubject:        env.Get("NATS_SUBJECT_LANE_STREAM", "twitch.ingress.event.stream"),
		ClipCreatedSubject:       env.Get("NATS_SUBJECT_CLIP_CREATED", "data.twitch.clip.created"),
		UserChangedSubject:       env.Get("NATS_SUBJECT_USER_CHANGED", "data.users.changed"),
	}
}
