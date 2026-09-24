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

	DiscordOutgressRPCPrefix string
	TwitchOutgressRPCPrefix  string
	DiscordDataRPCPrefix     string

	StreamLaneSubject  string
	ClipCreatedSubject string
	UserChangedSubject string
}

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
