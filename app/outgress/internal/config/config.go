package config

import (
	"ItsBagelBot/pkg/env"
)

type Config struct {
	NATSURL         string
	PremiumSubject  string
	StandardSubject string
	SystemSubject   string
	RPCPrefix       string

	// Worker pool sizes per lane. Premium gets the most goroutines so
	// broadcasters with large audiences drain ahead of standard and system
	// traffic (premium-first ordering). Standard and system carry lower
	// volumes and default to smaller pools.
	PremiumWorkers  int
	StandardWorkers int
	SystemWorkers   int

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
	return &Config{
		NATSURL:               env.Get("NATS_URL", "nats://127.0.0.1:4222"),
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
		PremiumWorkers:        env.GetInt("OUTGRESS_PREMIUM_WORKERS", 8),
		StandardWorkers:       env.GetInt("OUTGRESS_STANDARD_WORKERS", 3),
		SystemWorkers:         env.GetInt("OUTGRESS_SYSTEM_WORKERS", 2),
	}
}
