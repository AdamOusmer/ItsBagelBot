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

	// Fortnite provider (!fnstats + !store), off by default behind
	// FORTNITE_ENABLED. Two upstreams: the shop rides fortnite-api.com's
	// public /v2/shop, stats ride api-fortnite.com (x-api-key). The key gates
	// only the stats endpoint, so a keyless provider runs shop-only (!store
	// works, !fnstats stays dark). SeasonStart manually overrides the "season"
	// stats window's start epoch; 0 (default) auto-resolves it hourly from the
	// stats upstream's own season endpoint.
	FortniteBaseURL        string
	FortniteStatsBaseURL   string
	FortniteAPIKey         string
	FortniteEnabled        bool
	FortniteRateLimit      float64
	FortniteStatsRateLimit float64
	FortniteSeasonStart    int64

	// Govee smart-light provider. It holds no service key (each broadcaster
	// brings their own, fetched from the modules service). GoveeKeySubjectPrefix
	// is that internal RPC's subject prefix; empty disables the provider.
	GoveeBaseURL          string
	GoveeRateLimit        float64
	GoveeKeySubjectPrefix string

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

		FortniteBaseURL:      env.Get("FORTNITE_BASE_URL", "https://fortnite-api.com"),
		FortniteStatsBaseURL: env.Get("FORTNITE_STATS_BASE_URL", "https://prod.api-fortnite.com"),
		FortniteAPIKey:       env.Get("FORTNITE_API_KEY", ""),
		FortniteEnabled:      env.GetBool("FORTNITE_ENABLED", false),
		// Shop budget: fortnite-api.com publishes no hard per-key budget;
		// requests per minute.
		FortniteRateLimit: env.GetFloat("FORTNITE_RATE_LIMIT", 120.0),
		// Stats budget: api-fortnite.com's free plan allows 10k requests per
		// day; the default leaves headroom.
		FortniteStatsRateLimit: env.GetFloat("FORTNITE_STATS_RATE_LIMIT", 9000.0),
		FortniteSeasonStart:    int64(env.GetInt("FORTNITE_SEASON_START_UNIX", 0)),

		GoveeBaseURL:          env.Get("GOVEE_BASE_URL", "https://openapi.api.govee.com"),
		GoveeRateLimit:        env.GetFloat("GOVEE_RATE_LIMIT", 8.0),
		GoveeKeySubjectPrefix: env.Get("NATS_INTERNAL_GOVEE_KEY_SUBJECT_PREFIX", "bagel.rpc.internal.govee.key"),

		ListenAddr: env.Get("LISTEN_ADDR", ":8080"),
	}
}
