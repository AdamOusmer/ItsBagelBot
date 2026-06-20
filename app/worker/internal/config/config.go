// Package config loads the worker's runtime settings from the environment.
//
// The worker sits between ingress and outgress: it drains the three ingress
// lanes (premium, standard, stream), runs each event through the pipeline, and
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

	// Ingress lanes the worker consumes: premium and standard. Both carry every
	// actionable event laned by broadcaster status, including the live
	// (stream.online/offline) events ingress dual-publishes onto them, so the
	// worker never reads the dedicated stream lane. Must match
	// Ingress.Config.lane_subject/1.
	PremiumSubject  string
	StandardSubject string

	// The one consumer drains both lanes into a shared, autoscaling pool of
	// pipeline routines. MinRoutines/MaxRoutines bound the routines per
	// consumer, MaxConsumers caps how many consumers spin up once routines are
	// maxed, and the ScaleAfter windows pace growth and shrink. PremiumReserve
	// is the percentage of the pool kept for premium so a standard flood never
	// starves premium broadcasters.
	MinRoutines    int
	MaxRoutines    int
	MaxConsumers   int
	ScaleUpAfter   time.Duration
	ScaleDownAfter time.Duration
	PremiumReserve int

	// Outgress lane subjects the pipeline publishes onto. The lane is chosen
	// from the event's regress status (premium vs standard), so a premium
	// broadcaster's reply rides the premium outgress lane end to end.
	OutgressPremiumSubject  string
	OutgressStandardSubject string

	// Projection RPC subjects: the contracts the worker uses to resolve the
	// user, modules and commands for the broadcaster an event belongs to.
	// Reads hit the in-process cache first, then Valkey, then these RPCs as a
	// lazy-load fallback (see internal/projection).
	ProjectionUsersSubject    string
	ProjectionModulesSubject  string
	ProjectionCommandsSubject string

	// CacheInvalidationPrefix is the NATS subject prefix the worker subscribes
	// to for push invalidation of its in-process projection cache. Messages
	// arrive as "<prefix>.<scope>" with payload {"broadcaster_id":"<id>"}.
	CacheInvalidationPrefix string

	// Valkey holds the settings projection (user tier + modules) the worker
	// reads on the hot path.
	ValkeyAddr     string
	ValkeyPassword string

	ListenAddr string
}

func Load() *Config {
	natsURL := env.Get("NATS_URL", "nats://127.0.0.1:4222")
	return &Config{
		NATSURL:    natsURL,
		NATSRPCURL: env.Get("NATS_RPC_URL", natsURL),

		PremiumSubject:  env.Get("NATS_INGRESS_PREMIUM_SUBJECT", "twitch.ingress.event.premium"),
		StandardSubject: env.Get("NATS_INGRESS_STANDARD_SUBJECT", "twitch.ingress.event.standard"),

		MinRoutines:    env.GetInt("WORKER_MIN_ROUTINES", 2),
		MaxRoutines:    env.GetInt("WORKER_MAX_ROUTINES", 8),
		MaxConsumers:   env.GetInt("WORKER_MAX_CONSUMERS", 3),
		ScaleUpAfter:   env.GetDuration("WORKER_SCALE_UP_AFTER", 5*time.Second),
		ScaleDownAfter: env.GetDuration("WORKER_SCALE_DOWN_AFTER", 30*time.Second),
		PremiumReserve: env.GetInt("WORKER_PREMIUM_RESERVE_PERCENT", 25),

		OutgressPremiumSubject:  env.Get("NATS_OUTGRESS_PREMIUM_SUBJECT", "twitch.outgress.premium"),
		OutgressStandardSubject: env.Get("NATS_OUTGRESS_STANDARD_SUBJECT", "twitch.outgress.standard"),

		ProjectionUsersSubject:    env.Get("NATS_INTERNAL_PROJECTION_USERS_SUBJECT", "bagel.rpc.internal.projection.users.get"),
		ProjectionModulesSubject:  env.Get("NATS_INTERNAL_PROJECTION_MODULES_SUBJECT", "bagel.rpc.internal.projection.modules.get"),
		ProjectionCommandsSubject: env.Get("NATS_INTERNAL_PROJECTION_COMMANDS_SUBJECT", "bagel.rpc.internal.projection.commands.get"),

		CacheInvalidationPrefix: env.Get("NATS_CACHE_INVALIDATION_PREFIX", "bagel.cache.invalidate"),

		ValkeyAddr:     env.Get("VALKEY_ADDR", "127.0.0.1:6379"),
		ValkeyPassword: env.Get("VALKEY_PASSWORD", ""),

		ListenAddr: env.Get("LISTEN_ADDR", ":8080"),
	}
}
