// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// Package config loads the gossip service's runtime settings from the environment.
//
// The gossip service is the fleet's one door to external API systems: sesame asks it
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
	// Mojang is a second, independently-throttled upstream on this provider's
	// path, so it carries its own budget: Hypixel meters per key, Mojang meters
	// per source IP, and a name resolve spends Mojang's allowance before the
	// Hypixel key is ever touched.
	HypixelBaseURL   string
	MojangBaseURL    string
	HypixelAPIKey    string
	HypixelRateLimit float64
	MojangRateLimit  float64

	// MCSR Ranked provider. The public API needs no key; APIKey optionally
	// unlocks expanded rate limits. Enabled unless MCSR_ENABLED=false.
	McsrBaseURL   string
	McsrAPIKey    string
	McsrEnabled   bool
	McsrRateLimit float64

	// PaceMan provider (speedrun pace tracking for !pace/!nethers/!lastfort,
	// stacked on the mcsr module's linked account). The public API needs no
	// key at all, so unlike urchin/hypixel there is nothing to gate on: it
	// stays enabled unless an operator explicitly flips PACEMAN_ENABLED=false.
	// PaceMan rate-limits per client IP in a fixed 60-second window (180 for
	// player-stat routes, 120 for cursor histories); the default takes the
	// stricter class.
	PacemanBaseURL     string
	PacemanUserBaseURL string
	PacemanEnabled     bool
	PacemanRateLimit   float64

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

	// Valorant provider (rank/MMR, recent matches, leaderboards, account
	// lookups, featured-bundle viewer) riding the community HenrikDev API. Key
	// empty = provider disabled. The bundle viewer additionally reads Riot's
	// keyless content CDN (valorant-api.com) to turn item UUIDs into names,
	// icons and rarity colours — a second host with its own budget because it
	// meters per source IP while HenrikDev meters per key.
	ValorantBaseURL          string
	ValorantContentBaseURL   string
	ValorantAPIKey           string
	ValorantRateLimit        float64
	ValorantContentRateLimit float64

	// Govee smart-light provider. It holds no service key (each broadcaster
	// brings their own, fetched from the modules service). GoveeKeySubjectPrefix
	// is that internal RPC's subject prefix; empty disables the provider.
	GoveeBaseURL          string
	GoveeRateLimit        float64
	GoveeKeySubjectPrefix string

	// Clash Royale provider (!cr / !crstats / !crdecks / !crranked /
	// !crtrophy): the official Supercell player API through RoyaleAPI's
	// supported proxy. APIKey is a standard Supercell key created on
	// developer.clashroyale.com whose allowed-IP list names RoyaleAPI's proxy
	// egress 45.79.218.79 (not ours) so calls may forward through
	// proxy.royaleapi.dev; empty = provider disabled. Neither Supercell nor
	// the proxy publishes a hard per-key rate number, so the budget assumes a
	// trusted key and CLASHROYALE_RATE_LIMIT must be lowered for a fresh one.
	ClashRoyaleBaseURL   string
	ClashRoyaleAPIKey    string
	ClashRoyaleRateLimit float64

	ListenAddr string
}

func Load() *Config {
	natsURL := env.Get("NATS_URL", "nats://127.0.0.1:4222")
	return &Config{
		NATSURL:    natsURL,
		NATSRPCURL: env.Get("NATS_RPC_URL", natsURL),

		// Hard cutover from the gateway rename: no NATS_GATEWAY_SUBJECT_PREFIX
		// fallback. The NATS account/user were renamed too, so a stale prefix
		// would resolve against ACLs the old credential no longer has; delete
		// any leftover NATS_GATEWAY_SUBJECT_PREFIX from Doppler.
		SubjectPrefix: env.Get("NATS_GOSSIP_SUBJECT_PREFIX", "bagel.rpc.gossip"),

		ValkeyAddr:     env.Get("VALKEY_ADDR", "127.0.0.1:6379"),
		ValkeyPassword: env.Get("VALKEY_PASSWORD", ""),

		UrchinBaseURL: env.Get("URCHIN_BASE_URL", "https://api.urchin.gg"),
		UrchinAPIKey:  env.Get("URCHIN_API_KEY", ""),
		// Coral meters per key over a ROLLING 5-minute window: personal keys
		// allow 600, developer keys whatever was assigned at issue time. The
		// default fits a personal key; a developer key MUST set
		// URCHIN_RATE_LIMIT to its assigned number, and a key shared with an
		// overlay needs headroom below that (the budget is per key, not per
		// caller, so overlay polling spends the same window).
		UrchinRateLimit: env.GetFloat("URCHIN_RATE_LIMIT", 600.0),

		HypixelBaseURL: env.Get("HYPIXEL_BASE_URL", "https://api.hypixel.net"),
		MojangBaseURL:  env.Get("MOJANG_BASE_URL", "https://api.mojang.com"),
		HypixelAPIKey:  env.Get("HYPIXEL_API_KEY", ""),
		// Hypixel personal keys allow 300 requests per 5 minutes.
		HypixelRateLimit: env.GetFloat("HYPIXEL_RATE_LIMIT", 300.0),
		// Mojang throttles the profile endpoint per source IP, not per key, so
		// the whole fleet shares one allowance no matter how many pods run.
		// 600 per 10 minutes is the commonly observed ceiling; the bucket is
		// deliberately fleet-wide (one shared budget) rather than per-pod.
		MojangRateLimit: env.GetFloat("MOJANG_RATE_LIMIT", 600.0),

		McsrBaseURL:   env.Get("MCSR_BASE_URL", "https://api.mcsrranked.com"),
		McsrAPIKey:    env.Get("MCSR_API_KEY", ""),
		McsrEnabled:   env.GetBool("MCSR_ENABLED", true),
		McsrRateLimit: env.GetFloat("MCSR_RATE_LIMIT", 500.0),

		PacemanBaseURL:     env.Get("PACEMAN_BASE_URL", "https://paceman.gg/stats/api"),
		PacemanUserBaseURL: env.Get("PACEMAN_USER_BASE_URL", "https://paceman.gg/api/us"),
		PacemanEnabled:     env.GetBool("PACEMAN_ENABLED", true),
		PacemanRateLimit:   env.GetFloat("PACEMAN_RATE_LIMIT", 120.0),

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

		ValorantBaseURL:        env.Get("VALORANT_BASE_URL", "https://api.henrikdev.xyz"),
		ValorantContentBaseURL: env.Get("VALORANT_CONTENT_BASE_URL", "https://valorant-api.com"),
		ValorantAPIKey:         env.Get("VALORANT_API_KEY", ""),
		// The fleet runs HenrikDev's instant Basic tier: 30 requests/min. The
		// default sits AT the ceiling because every gossip pod shares one key,
		// and the local bucket denying at 30 is strictly cheaper than the
		// upstream's own 429 poisoning any cache fill in flight. Upgrading the
		// Doppler key to Enhanced (90/min) MUST be paired with raising this to
		// ~80, or a third of the paid allowance goes unused.
		ValorantRateLimit: env.GetFloat("VALORANT_RATE_LIMIT", 30.0),
		// valorant-api.com publishes no hard per-client limit; 60/min is
		// conservative for a multi-MB skins payload that caches for a day.
		ValorantContentRateLimit: env.GetFloat("VALORANT_CONTENT_RATE_LIMIT", 60.0),

		GoveeBaseURL:          env.Get("GOVEE_BASE_URL", "https://openapi.api.govee.com"),
		GoveeRateLimit:        env.GetFloat("GOVEE_RATE_LIMIT", 8.0),
		GoveeKeySubjectPrefix: env.Get("NATS_INTERNAL_GOVEE_KEY_SUBJECT_PREFIX", "bagel.rpc.internal.govee.key"),

		ClashRoyaleBaseURL:   env.Get("CLASHROYALE_BASE_URL", "https://proxy.royaleapi.dev/v1"),
		ClashRoyaleAPIKey:    env.Get("CLASHROYALE_API_KEY", ""),
		ClashRoyaleRateLimit: env.GetFloat("CLASHROYALE_RATE_LIMIT", 600.0),

		ListenAddr: env.Get("LISTEN_ADDR", ":8080"),
	}
}
