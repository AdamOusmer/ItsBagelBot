// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package config

import (
	"strings"
	"time"

	"ItsBagelBot/pkg/env"
	"ItsBagelBot/pkg/env/conf"
	"ItsBagelBot/pkg/svcboot"
)

type Config struct {
	svcboot.Infra

	// A name other than the existing durable's replays the whole DeliverAll stream.
	ConsumerName string

	conf.Lanes

	MinRoutines    int
	MaxRoutines    int
	MinConsumers   int
	MaxConsumers   int
	ScaleUpAfter   time.Duration
	ScaleDownAfter time.Duration
	PremiumReserve int

	// DrainTimeout must stay below the pod's terminationGracePeriodSeconds.
	DrainTimeout time.Duration

	OutgressSystemSubject string
	ProjectionLiveSubject string
	SpecialUserIDs        string
	AutoRefundChannel     string
	BotUserID             string

	AutomodEnforce      bool
	ShieldEnabled       bool
	EmotesEnabled       bool
	NukeEnabled         bool
	AdaptiveEnabled     bool
	LinkCheckEnabled    bool
	LinkCheckFeeds      bool
	LinkCheckShorteners bool

	LiveTTL time.Duration

	IdempotencyEnabled bool
	// Must exceed stream MaxAge plus the retry hop, or late redeliveries run effects twice.
	IdempotencyTTL time.Duration

	conf.Projection

	CommandsDashboardPrefix string
	ModulesRPCPrefix        string
	PublicBaseURL           string
	GossipRPCPrefix         string
	LoyaltyRPCPrefix        string
	OutgressRPCPrefix       string
}

func Load() *Config {
	return &Config{
		Infra: svcboot.LoadInfra(),

		ConsumerName: env.Get("SESAME_CONSUMER_NAME", "worker"),

		Lanes:      conf.LoadLanes(),
		Projection: conf.LoadProjectionViaProjector(),

		MinRoutines:    env.GetInt("SESAME_MIN_ROUTINES", 2),
		MaxRoutines:    env.GetInt("SESAME_MAX_ROUTINES", 8),
		MinConsumers:   env.GetInt("SESAME_MIN_CONSUMERS", 1),
		MaxConsumers:   env.GetInt("SESAME_MAX_CONSUMERS", 3),
		ScaleUpAfter:   env.GetDuration("SESAME_SCALE_UP_AFTER", 5*time.Second),
		ScaleDownAfter: env.GetDuration("SESAME_SCALE_DOWN_AFTER", 30*time.Second),
		PremiumReserve: env.GetInt("SESAME_PREMIUM_RESERVE_PERCENT", 25),

		DrainTimeout: env.GetDuration("SESAME_DRAIN_TIMEOUT", 25*time.Second),

		OutgressSystemSubject: env.Get("NATS_OUTGRESS_SYSTEM_SUBJECT", "twitch.outgress.system"),

		ProjectionLiveSubject: env.Get("NATS_BROADCASTER_LIVE_SUBJECT", "bagel.rpc.broadcaster.live.get"),

		SpecialUserIDs: env.Get("TWITCH_SPECIAL_USER_IDS", ""),

		AutoRefundChannel: env.Get("TWITCH_AUTOREFUND_CHANNEL", ""),

		BotUserID: env.Get("TWITCH_BOT_USER_ID", ""),

		AutomodEnforce:  env.Get("SESAME_AUTOMOD_ENFORCE", "false") == "true",
		ShieldEnabled:   env.Get("SESAME_AUTOMOD_SHIELD", "false") == "true",
		EmotesEnabled:   env.Get("SESAME_AUTOMOD_EMOTES", "true") == "true",
		AdaptiveEnabled: env.Get("SESAME_AUTOMOD_ADAPTIVE", "false") == "true",

		LinkCheckEnabled:    env.GetBool("SESAME_LINKCHECK", false),
		LinkCheckFeeds:      env.GetBool("SESAME_LINKCHECK_FEEDS", true),
		LinkCheckShorteners: env.GetBool("SESAME_LINKCHECK_SHORTENERS", true),

		NukeEnabled: env.Get("SESAME_NUKE", "on") != "off",

		LiveTTL: env.GetDuration("SESAME_LIVE_TTL", 12*time.Hour),

		IdempotencyEnabled: env.Get("SESAME_IDEMPOTENCY", "on") != "off",
		IdempotencyTTL:     env.GetDuration("SESAME_IDEMPOTENCY_TTL", 15*time.Minute),

		CommandsDashboardPrefix: env.Get("NATS_COMMANDS_DASHBOARD_PREFIX", "bagel.rpc.commands"),

		ModulesRPCPrefix: env.Get("NATS_MODULES_SUBJECT_PREFIX", "bagel.rpc.modules"),

		PublicBaseURL: strings.TrimRight(env.Get("SESAME_PUBLIC_BASE_URL", "https://commands.itsbagelbot.com"), "/"),

		GossipRPCPrefix: env.Get("NATS_GOSSIP_SUBJECT_PREFIX", "bagel.rpc.gossip"),

		LoyaltyRPCPrefix: env.Get("NATS_LOYALTY_SUBJECT_PREFIX", "bagel.rpc.loyalty"),

		OutgressRPCPrefix: env.Get("NATS_OUTGRESS_RPC_PREFIX", "bagel.rpc.outgress"),
	}
}
