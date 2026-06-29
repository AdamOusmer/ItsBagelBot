// Package config loads the worker's runtime settings from the environment.
//
// The worker sits between ingress and outgress: it drains the two of ingress
// lanes (premium, standard), runs each event through the pipeline, and
// publishes the resulting actions onto the outgress lanes. Every knob here is
// a plain env var with a development-friendly default, mirroring the outgress
// service so the two read the same way.
package config

import (
	"time"

	"ItsBagelBot/pkg/env"
)

type Config struct {
	NATSURL    string
	NATSRPCURL string

	PremiumSubject  string
	StandardSubject string

	MaxRoutines    int
	MaxConsumers   int
	ScaleUpAfter   time.Duration
	ScaleDownAfter time.Duration
	PremiumReserve int

	OutgressPremiumSubject  string
	OutgressStandardSubject string

	ProjectionLiveSubject string

	SpecialUserIDs string

	LiveTTL time.Duration

	ProjectionUsersSubject    string
	ProjectionModulesSubject  string
	ProjectionCommandsSubject string

	CacheInvalidationPrefix string

	ValkeyAddr     string
	ValkeyPassword string
}

func Load() *Config {
	natsURL := env.Get("NATS_URL", "nats://127.0.0.1:4222")
	return &Config{
		NATSURL:    natsURL,
		NATSRPCURL: env.Get("NATS_RPC_URL", natsURL),

		PremiumSubject:  env.Get("NATS_INGRESS_PREMIUM_SUBJECT", "twitch.ingress.event.premium"),
		StandardSubject: env.Get("NATS_INGRESS_STANDARD_SUBJECT", "twitch.ingress.event.standard"),

		MaxRoutines:    env.GetInt("WORKER_MAX_ROUTINES", 50),
		MaxConsumers:   env.GetInt("WORKER_MAX_CONSUMERS", 3),
		ScaleUpAfter:   env.GetDuration("WORKER_SCALE_UP_AFTER", 5*time.Second),
		ScaleDownAfter: env.GetDuration("WORKER_SCALE_DOWN_AFTER", 30*time.Second),
		PremiumReserve: env.GetInt("WORKER_PREMIUM_RESERVE_PERCENT", 25),

		OutgressPremiumSubject:  env.Get("NATS_OUTGRESS_PREMIUM_SUBJECT", "twitch.outgress.premium"),
		OutgressStandardSubject: env.Get("NATS_OUTGRESS_STANDARD_SUBJECT", "twitch.outgress.standard"),

		SpecialUserIDs: env.Get("TWITCH_SPECIAL_USER_IDS", ""),

		LiveTTL: env.GetDuration("WORKER_LIVE_TTL", 12*time.Hour),

		ProjectionUsersSubject:    env.Get("NATS_INTERNAL_PROJECTION_USERS_SUBJECT", "bagel.rpc.internal.projection.users.get"),
		ProjectionModulesSubject:  env.Get("NATS_INTERNAL_PROJECTION_MODULES_SUBJECT", "bagel.rpc.internal.projection.modules.get"),
		ProjectionCommandsSubject: env.Get("NATS_INTERNAL_PROJECTION_COMMANDS_SUBJECT", "bagel.rpc.internal.projection.commands.get"),

		CacheInvalidationPrefix: env.Get("NATS_CACHE_INVALIDATION_PREFIX", "bagel.cache.invalidate"),

		ValkeyAddr:     env.Get("VALKEY_ADDR", "127.0.0.1:6379"),
		ValkeyPassword: env.Get("VALKEY_PASSWORD", ""),
	}
}
