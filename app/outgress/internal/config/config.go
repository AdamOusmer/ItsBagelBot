package config

import (
	"time"

	"ItsBagelBot/pkg/env"
)

type Config struct {
	NATSURL         string
	NATSRPCURL      string
	PremiumSubject  string
	StandardSubject string
	SystemSubject   string
	RPCPrefix       string

	// The central premium + standard consumer autoscales its routine pool.
	// MinRoutines/MaxRoutines bound the routines per consumer; MaxConsumers
	// caps how many consumers spin up once routines are maxed; the ScaleAfter
	// windows pace growth and shrink. PremiumReserve is the percentage of the
	// pool kept for premium so a standard flood never starves it.
	MinRoutines    int
	MaxRoutines    int
	MaxConsumers   int
	ScaleUpAfter   time.Duration
	ScaleDownAfter time.Duration
	PremiumReserve int

	// SystemWorkers sizes the system lane's own, independent consumer (the
	// dashboard's EventSub create/delete jobs), kept off the weighted budget.
	SystemWorkers int

	ValkeyAddr     string
	ValkeyPassword string

	TwitchClientID     string
	TwitchClientSecret string

	// TwitchConduitID is the Conduit the eventsub jobs manage subscriptions
	// on. Without it eventsub jobs are dropped; chat and api traffic is
	// unaffected.
	TwitchConduitID string

	// TwitchBotUserID identifies the bot account for moderation lookups.
	// When empty, the sender_id carried by each message is used instead.
	TwitchBotUserID string

	// TwitchBotRefreshToken unlocks user-token endpoints (mod status
	// verification). Optional: without it the service runs on the app token
	// alone and treats unverified channels as non-mod, which never
	// over-sends. When TwitchBotUserID is set this is only the seed; the
	// stored token managed through the admin panel takes precedence.
	TwitchBotRefreshToken string

	// TokensSubjectPrefix is the users service token RPC outgress loads the
	// bot account's token from and persists rotations back to.
	TokensSubjectPrefix string
}

func Load() *Config {
	natsURL := env.Get("NATS_URL", "nats://127.0.0.1:4222")
	return &Config{
		NATSURL:               natsURL,
		NATSRPCURL:            env.Get("NATS_RPC_URL", natsURL),
		PremiumSubject:        env.Get("NATS_OUTGRESS_PREMIUM_SUBJECT", "twitch.outgress.premium"),
		StandardSubject:       env.Get("NATS_OUTGRESS_STANDARD_SUBJECT", "twitch.outgress.standard"),
		SystemSubject:         env.Get("NATS_OUTGRESS_SYSTEM_SUBJECT", "twitch.outgress.system"),
		RPCPrefix:             env.Get("NATS_OUTGRESS_RPC_PREFIX", "bagel.rpc.outgress"),
		ValkeyAddr:            env.Get("VALKEY_ADDR", "127.0.0.1:6379"),
		ValkeyPassword:        env.Get("VALKEY_PASSWORD", ""),
		TwitchClientID:        env.MustGet("TWITCH_CLIENT_ID"),
		TwitchClientSecret:    env.MustGet("TWITCH_CLIENT_SECRET"),
		TwitchConduitID:       env.Get("TWITCH_CONDUIT_ID", ""),
		TwitchBotUserID:       env.Get("TWITCH_BOT_USER_ID", ""),
		TwitchBotRefreshToken: env.Get("TWITCH_BOT_REFRESH_TOKEN", ""),
		TokensSubjectPrefix:   env.Get("NATS_INTERNAL_TOKENS_SUBJECT_PREFIX", "bagel.rpc.internal.tokens"),
		MinRoutines:           env.GetInt("OUTGRESS_MIN_ROUTINES", 2),
		MaxRoutines:           env.GetInt("OUTGRESS_MAX_ROUTINES", 8),
		MaxConsumers:          env.GetInt("OUTGRESS_MAX_CONSUMERS", 3),
		ScaleUpAfter:          env.GetDuration("OUTGRESS_SCALE_UP_AFTER", 5*time.Second),
		ScaleDownAfter:        env.GetDuration("OUTGRESS_SCALE_DOWN_AFTER", 30*time.Second),
		PremiumReserve:        env.GetInt("OUTGRESS_PREMIUM_RESERVE_PERCENT", 25),
		SystemWorkers:         env.GetInt("OUTGRESS_SYSTEM_WORKERS", 2),
	}
}
