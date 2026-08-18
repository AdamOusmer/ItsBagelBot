// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// Package config loads sesame's runtime settings from the environment.
//
// sesame sits between ingress and outgress: it drains the ingress premium and
// standard lanes, runs each event through the engine pipeline, and publishes the
// resulting actions onto the outgress lanes. Every knob is a plain env var with a
// development-friendly default.
//
// Secret-provided vars (VALKEY_*, NATS_CACHE_INVALIDATION_PREFIX,
// TWITCH_SPECIAL_USER_IDS, TWITCH_BOT_USER_ID) keep the exact names the worker
// used, so the same Doppler config supplies them unchanged. Only the pod-tuning
// knobs are renamed WORKER_* -> SESAME_* (set in sesame's own manifest).
package config

import (
	"strings"
	"time"

	"ItsBagelBot/pkg/env"
)

type Config struct {
	NATSURL    string
	NATSRPCURL string

	// ConsumerName is the JetStream durable/queue group the subscriber binds. It
	// defaults to "worker" so sesame reuses the worker's existing lane consumer:
	// the lane consumers are DeliverAll, so a fresh durable would replay the whole
	// stream, and reusing the group means rollout overlap load-balances across the
	// shared DeliverGroup instead of double-processing. It is a genuine drop-in on
	// the same lanes and the same pkg/bus consumer.
	ConsumerName string

	// The one consumer drains both lanes into a shared, autoscaling pool of
	// pipeline routines. PremiumReserve keeps a slice of the pool for premium so a
	// standard flood never starves premium broadcasters.
	MinRoutines    int
	MaxRoutines    int
	MinConsumers   int
	MaxConsumers   int
	ScaleUpAfter   time.Duration
	ScaleDownAfter time.Duration
	PremiumReserve int

	// DrainTimeout bounds how long shutdown waits for handlers already dispatched
	// to finish after SIGTERM stops the consumer pulling. Keep it below the pod's
	// terminationGracePeriodSeconds so the drain completes before the kubelet
	// SIGKILLs the process. A handler that outlives the deadline is abandoned and
	// its event redelivered; outputs already stored retain deterministic NATS IDs
	// and are folded by the broker.
	DrainTimeout time.Duration

	// SpecialUserIDs is the comma-separated list of special (bagel-crew) Twitch
	// user ids, the same Doppler secret ingress uses to lane them premium.
	SpecialUserIDs string

	// BotUserID is the bot's own Twitch user id; the engine skips the bot's own
	// chat messages so it never reacts to itself.
	BotUserID string

	// AutomodEnforce arms the automod: false (default) runs it in shadow mode
	// (verdicts are logged, no action taken); true emits the ban/timeout actions.
	AutomodEnforce bool

	// ShieldEnabled lets a confirmed mass-raid escalate to channel-level Shield
	// Mode. It is stricter than AutomodEnforce (Shield Mode is aggressive and
	// broadcaster-visible) and only takes effect when AutomodEnforce is also on.
	// Off by default.
	ShieldEnabled bool

	// EmotesEnabled starts the background third-party emote-set refresher (BTTV,
	// FFZ, 7TV global sets) that feeds the automod's caps-heuristic false-positive
	// suppression. On by default; the endpoints are small, public and unauthenticated.
	EmotesEnabled bool

	// LiveTTL bounds how long a live key survives without a refresh.
	LiveTTL time.Duration

	// IdempotencyEnabled arms the consumer-side dedup guard (SESAME_IDEMPOTENCY,
	// on by default). off is the kill switch: the guard fails open everywhere and
	// no claim is written. IdempotencyTTL bounds a claim; it must exceed the widest
	// replay window (the stream MaxAge plus the retry hop) so a late redelivery is
	// still recognised — 15m covers the 5m firehose MaxAge and the ~30s retry TTL
	// with margin.
	IdempotencyEnabled bool
	IdempotencyTTL     time.Duration

	// PublicBaseURL is the origin of the public console pages. The !cmd module
	// builds the channel command-page link from it as "<base>/user/<broadcaster
	// id>". Stored without a trailing slash.
	PublicBaseURL string

	// Valkey holds the settings projection (user tier + modules) sesame reads on
	// the hot path.
	ValkeyAddr     string
	ValkeyPassword string

	ListenAddr string
}

func Load() *Config {
	natsURL := env.Get("NATS_URL", "nats://127.0.0.1:4222")
	return &Config{
		NATSURL:    natsURL,
		NATSRPCURL: env.Get("NATS_RPC_URL", natsURL),

		ConsumerName: env.Get("SESAME_CONSUMER_NAME", "worker"),

		MinRoutines:    env.GetInt("SESAME_MIN_ROUTINES", 2),
		MaxRoutines:    env.GetInt("SESAME_MAX_ROUTINES", 8),
		MinConsumers:   env.GetInt("SESAME_MIN_CONSUMERS", 1),
		MaxConsumers:   env.GetInt("SESAME_MAX_CONSUMERS", 3),
		ScaleUpAfter:   env.GetDuration("SESAME_SCALE_UP_AFTER", 5*time.Second),
		ScaleDownAfter: env.GetDuration("SESAME_SCALE_DOWN_AFTER", 30*time.Second),
		PremiumReserve: env.GetInt("SESAME_PREMIUM_RESERVE_PERCENT", 25),

		DrainTimeout: env.GetDuration("SESAME_DRAIN_TIMEOUT", 25*time.Second),

		SpecialUserIDs: env.Get("TWITCH_SPECIAL_USER_IDS", ""),

		BotUserID: env.Get("TWITCH_BOT_USER_ID", ""),

		AutomodEnforce: env.Get("SESAME_AUTOMOD_ENFORCE", "false") == "true",
		ShieldEnabled:  env.Get("SESAME_AUTOMOD_SHIELD", "false") == "true",
		EmotesEnabled:  env.Get("SESAME_AUTOMOD_EMOTES", "true") == "true",

		LiveTTL: env.GetDuration("SESAME_LIVE_TTL", 12*time.Hour),

		IdempotencyEnabled: env.Get("SESAME_IDEMPOTENCY", "on") != "off",
		IdempotencyTTL:     env.GetDuration("SESAME_IDEMPOTENCY_TTL", 15*time.Minute),

		PublicBaseURL: strings.TrimRight(env.Get("SESAME_PUBLIC_BASE_URL", "https://dashboard.itsbagelbot.com"), "/"),

		ValkeyAddr:     env.Get("VALKEY_ADDR", "127.0.0.1:6379"),
		ValkeyPassword: env.Get("VALKEY_PASSWORD", ""),

		ListenAddr: env.Get("LISTEN_ADDR", ":8080"),
	}
}
