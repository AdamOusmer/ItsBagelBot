// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package config

import "ItsBagelBot/pkg/env"

// RoleGateway holds the single Discord gateway Identify session (welcomes,
// join-to-create voice, tickets, staff logs, crumb ranks, slash commands).
// Exactly one replica may run this role: two Identify sessions on one bot
// token fight each other for the connection.
const RoleGateway = "gateway"

// RoleEgress makes Discord REST calls off NATS: go-live/offline embeds, the
// clip archive post, and the guild setup/layout RPC. No gateway session, so
// it scales to multiple replicas freely.
const RoleEgress = "egress"

// Config is the process env dingress boots from.
type Config struct {
	Role            string
	ListenAddr      string
	ValkeyAddr      string
	ValkeyPassword  string
	DiscordBotToken string

	// NATSURL/NATSRPCURL are only read in ROLE=egress: the gateway role never
	// dials NATS, so boot must not require it (see Load's default and
	// main.go's role dispatch).
	NATSURL    string
	NATSRPCURL string

	// RPCPrefix/RPCQueue address the guild setup/layout/unbind/post RPC,
	// ported from app/outgress/rpc/discord.go. Mirrors outgress's own
	// NATS_OUTGRESS_RPC_PREFIX naming, one service segment over.
	RPCPrefix string
	RPCQueue  string

	// OutgressRPCPrefix is outgress's own RPC prefix, not dingress's --
	// dingress is the CALLER here (the go-live embed's Helix-details
	// fallback, see internal/egress/live.go's liveInfo), the one caller
	// this service has of another service's RPC. Same default/env-var name
	// sesame's config already uses (NATS_OUTGRESS_RPC_PREFIX) so ops set one
	// knob for every consumer of outgress's management RPC.
	OutgressRPCPrefix string

	// StreamLaneSubject is the TWITCH_INGRESS subject carrying real Twitch
	// stream.online/offline EventSub messages. Egress binds its OWN durable
	// consumer here (same name as outgress's own env override, so ops can
	// point both at the same value without inventing a second knob) as a
	// plain second subscriber alongside outgress -- not a handoff.
	StreamLaneSubject string

	// ClipCreatedSubject is data.twitch.clip.created on BAGEL_DATA
	// (LimitsPolicy/FileStorage/R3). Egress binds its own durable pull
	// consumer; see internal/domain/event/data/clip_events.go for why this
	// is a fact and not a Discord command.
	ClipCreatedSubject string
}

// Load reads process env. Empty DISCORD_BOT_TOKEN leaves the service idle
// (both roles) with health still serving. ROLE defaults to gateway so an
// unset env var reproduces today's exact behavior.
func Load() Config {
	natsURL := env.Get("NATS_URL", "nats://127.0.0.1:4222")
	return Config{
		Role:               env.Get("ROLE", RoleGateway),
		ListenAddr:         env.Get("LISTEN_ADDR", ":8080"),
		ValkeyAddr:         env.Get("VALKEY_ADDR", "127.0.0.1:6379"),
		ValkeyPassword:     env.Get("VALKEY_PASSWORD", ""),
		DiscordBotToken:    env.Get("DISCORD_BOT_TOKEN", ""),
		NATSURL:            natsURL,
		NATSRPCURL:         env.Get("NATS_RPC_URL", natsURL),
		RPCPrefix:          env.Get("NATS_DINGRESS_RPC_PREFIX", "bagel.rpc.dingress"),
		RPCQueue:           env.Get("NATS_DINGRESS_RPC_QUEUE", "dingress-rpc"),
		OutgressRPCPrefix:  env.Get("NATS_OUTGRESS_RPC_PREFIX", "bagel.rpc.outgress"),
		StreamLaneSubject:  env.Get("NATS_SUBJECT_LANE_STREAM", "twitch.ingress.event.stream"),
		ClipCreatedSubject: env.Get("NATS_SUBJECT_CLIP_CREATED", "data.twitch.clip.created"),
	}
}
