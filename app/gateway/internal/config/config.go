// Package config loads the gateway's runtime settings from the environment.
//
// The gateway is the fleet's one door to external API systems: sesame asks it
// over NATS RPC, it fetches from the upstream (urchin.gg Coral, MCSR Ranked)
// and caches replies in Valkey. Providers with no credentials configured are
// skipped at boot, so a missing key degrades to "provider offline", never a
// crash loop.
package config

import (
	"ItsBagelBot/pkg/env"
)

type Config struct {
	NATSURL    string
	NATSRPCURL string

	// SubjectPrefix is the NATS prefix every provider endpoint subscribes
	// under: "<prefix>.<provider>.<endpoint>".
	SubjectPrefix string

	// Valkey holds the reply cache and the mcsr stream-session snapshots.
	ValkeyAddr     string
	ValkeyPassword string

	// Urchin (Coral) provider. APIKey empty = provider disabled.
	UrchinBaseURL   string
	UrchinAPIKey    string
	UrchinRateLimit float64

	// Hypixel provider (lifetime Bed Wars stats for !bwstats): its own external
	// system with its own key and budget — Coral's profile endpoint needs the
	// Player Data permission our key lacks (403). Key empty = provider disabled.
	// Usernames resolve to uuids through Mojang's public API.
	HypixelBaseURL   string
	MojangBaseURL    string
	HypixelAPIKey    string
	HypixelRateLimit float64

	// MCSR Ranked provider. The public API needs no key; APIKey optionally
	// unlocks expanded rate limits. Enabled unless MCSR_ENABLED=false.
	McsrBaseURL   string
	McsrAPIKey    string
	McsrEnabled   bool
	McsrRateLimit float64

	ListenAddr string
}

func Load() *Config {
	natsURL := env.Get("NATS_URL", "nats://127.0.0.1:4222")
	return &Config{
		NATSURL:    natsURL,
		NATSRPCURL: env.Get("NATS_RPC_URL", natsURL),

		SubjectPrefix: env.Get("NATS_GATEWAY_SUBJECT_PREFIX", "bagel.rpc.gateway"),

		ValkeyAddr:     env.Get("VALKEY_ADDR", "127.0.0.1:6379"),
		ValkeyPassword: env.Get("VALKEY_PASSWORD", ""),

		UrchinBaseURL:   env.Get("URCHIN_BASE_URL", "https://api.urchin.gg"),
		UrchinAPIKey:    env.Get("URCHIN_API_KEY", ""),
		UrchinRateLimit: env.GetFloat("URCHIN_RATE_LIMIT", 600.0),

		HypixelBaseURL: env.Get("HYPIXEL_BASE_URL", "https://api.hypixel.net"),
		MojangBaseURL:  env.Get("MOJANG_BASE_URL", "https://api.mojang.com"),
		HypixelAPIKey:  env.Get("HYPIXEL_API_KEY", ""),
		// Hypixel personal keys allow 300 requests per 5 minutes.
		HypixelRateLimit: env.GetFloat("HYPIXEL_RATE_LIMIT", 300.0),

		McsrBaseURL:   env.Get("MCSR_BASE_URL", "https://api.mcsrranked.com"),
		McsrAPIKey:    env.Get("MCSR_API_KEY", ""),
		McsrEnabled:   env.GetBool("MCSR_ENABLED", true),
		McsrRateLimit: env.GetFloat("MCSR_RATE_LIMIT", 500.0),

		ListenAddr: env.Get("LISTEN_ADDR", ":8080"),
	}
}
