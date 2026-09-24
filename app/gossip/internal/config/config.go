// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package config

import (
	"time"

	"ItsBagelBot/pkg/env"
	"ItsBagelBot/pkg/svcboot"
)

type Config struct {
	svcboot.Infra

	SubjectPrefix string

	UrchinBaseURL   string
	UrchinAPIKey    string
	UrchinRateLimit float64

	HypixelBaseURL   string
	MojangBaseURL    string
	HypixelAPIKey    string
	HypixelRateLimit float64
	MojangRateLimit  float64

	McsrBaseURL   string
	McsrAPIKey    string
	McsrEnabled   bool
	McsrRateLimit float64

	PacemanBaseURL     string
	PacemanUserBaseURL string
	PacemanEnabled     bool
	PacemanRateLimit   float64

	FortniteBaseURL        string
	FortniteStatsBaseURL   string
	FortniteAPIKey         string
	FortniteEnabled        bool
	FortniteRateLimit      float64
	FortniteStatsRateLimit float64
	FortniteSeasonStart    int64

	CODMBaseURL   string
	CODMCountry   string
	CODMEnabled   bool
	CODMRateLimit float64

	ValorantBaseURL          string
	ValorantContentBaseURL   string
	ValorantAPIKey           string
	ValorantRateLimit        float64
	ValorantContentRateLimit float64

	GoveeBaseURL          string
	GoveeRateLimit        float64
	GoveeKeySubjectPrefix string

	SpotifyBaseURL          string
	SpotifyAccountsURL      string
	SpotifyRateLimit        float64
	SpotifyKeySubjectPrefix string

	ClashRoyaleBaseURL   string
	ClashRoyaleAPIKey    string
	ClashRoyaleRateLimit float64

	FetchKeySubjectPrefix  string
	FetchProjectionSubject string

	CustomChannelRateLimit float64
	CustomDefRateLimit     float64
	CustomHostRateLimit    float64
	CustomPositiveTTL      time.Duration

	ListenAddr string
}

func Load() *Config {
	return &Config{
		Infra: svcboot.LoadInfra(),

		SubjectPrefix: env.Get("NATS_GOSSIP_SUBJECT_PREFIX", "bagel.rpc.gossip"),

		UrchinBaseURL:   env.Get("URCHIN_BASE_URL", "https://api.urchin.gg"),
		UrchinAPIKey:    env.Get("URCHIN_API_KEY", ""),
		UrchinRateLimit: env.GetFloat("URCHIN_RATE_LIMIT", 600.0),

		HypixelBaseURL:   env.Get("HYPIXEL_BASE_URL", "https://api.hypixel.net"),
		MojangBaseURL:    env.Get("MOJANG_BASE_URL", "https://api.mojang.com"),
		HypixelAPIKey:    env.Get("HYPIXEL_API_KEY", ""),
		HypixelRateLimit: env.GetFloat("HYPIXEL_RATE_LIMIT", 300.0),
		MojangRateLimit:  env.GetFloat("MOJANG_RATE_LIMIT", 600.0),

		McsrBaseURL:   env.Get("MCSR_BASE_URL", "https://api.mcsrranked.com"),
		McsrAPIKey:    env.Get("MCSR_API_KEY", ""),
		McsrEnabled:   env.GetBool("MCSR_ENABLED", true),
		McsrRateLimit: env.GetFloat("MCSR_RATE_LIMIT", 500.0),

		PacemanBaseURL:     env.Get("PACEMAN_BASE_URL", "https://paceman.gg/stats/api"),
		PacemanUserBaseURL: env.Get("PACEMAN_USER_BASE_URL", "https://paceman.gg/api/us"),
		PacemanEnabled:     env.GetBool("PACEMAN_ENABLED", true),
		PacemanRateLimit:   env.GetFloat("PACEMAN_RATE_LIMIT", 120.0),

		FortniteBaseURL:        env.Get("FORTNITE_BASE_URL", "https://fortnite-api.com"),
		FortniteStatsBaseURL:   env.Get("FORTNITE_STATS_BASE_URL", "https://prod.api-fortnite.com"),
		FortniteAPIKey:         env.Get("FORTNITE_API_KEY", ""),
		FortniteEnabled:        env.GetBool("FORTNITE_ENABLED", false),
		FortniteRateLimit:      env.GetFloat("FORTNITE_RATE_LIMIT", 120.0),
		FortniteStatsRateLimit: env.GetFloat("FORTNITE_STATS_RATE_LIMIT", 9000.0),
		FortniteSeasonStart:    int64(env.GetInt("FORTNITE_SEASON_START_UNIX", 0)),

		CODMBaseURL:   env.Get("CODM_BASE_URL", "https://order-sg.codashop.com"),
		CODMCountry:   env.Get("CODM_COUNTRY", "IN"),
		CODMEnabled:   env.GetBool("CODM_ENABLED", true),
		CODMRateLimit: env.GetFloat("CODM_RATE_LIMIT", 60.0),

		ValorantBaseURL:          env.Get("VALORANT_BASE_URL", "https://api.henrikdev.xyz"),
		ValorantContentBaseURL:   env.Get("VALORANT_CONTENT_BASE_URL", "https://valorant-api.com"),
		ValorantAPIKey:           env.Get("VALORANT_API_KEY", ""),
		ValorantRateLimit:        env.GetFloat("VALORANT_RATE_LIMIT", 30.0),
		ValorantContentRateLimit: env.GetFloat("VALORANT_CONTENT_RATE_LIMIT", 60.0),

		GoveeBaseURL:          env.Get("GOVEE_BASE_URL", "https://openapi.api.govee.com"),
		GoveeRateLimit:        env.GetFloat("GOVEE_RATE_LIMIT", 8.0),
		GoveeKeySubjectPrefix: env.Get("NATS_INTERNAL_GOVEE_KEY_SUBJECT_PREFIX", "bagel.rpc.internal.govee.key"),

		SpotifyBaseURL:          env.Get("SPOTIFY_BASE_URL", "https://api.spotify.com"),
		SpotifyAccountsURL:      env.Get("SPOTIFY_ACCOUNTS_URL", "https://accounts.spotify.com"),
		SpotifyRateLimit:        env.GetFloat("SPOTIFY_RATE_LIMIT", 30.0),
		SpotifyKeySubjectPrefix: env.Get("NATS_INTERNAL_SPOTIFY_KEY_SUBJECT_PREFIX", "bagel.rpc.internal.spotify.key"),

		ClashRoyaleBaseURL:   env.Get("CLASHROYALE_BASE_URL", "https://proxy.royaleapi.dev/v1"),
		ClashRoyaleAPIKey:    env.Get("CLASHROYALE_API_KEY", ""),
		ClashRoyaleRateLimit: env.GetFloat("CLASHROYALE_RATE_LIMIT", 600.0),

		FetchKeySubjectPrefix:  env.Get("NATS_INTERNAL_FETCH_KEY_SUBJECT_PREFIX", "bagel.rpc.internal.commands.fetchkey"),
		FetchProjectionSubject: env.Get("PROJECTION_FETCHES_SUBJECT", "bagel.rpc.internal.projection.commands.fetches.get"),

		CustomChannelRateLimit: env.GetFloat("CUSTOM_FETCH_CHANNEL_RATE_LIMIT", 6.0),
		CustomDefRateLimit:     env.GetFloat("CUSTOM_FETCH_DEF_RATE_LIMIT", 30.0),
		CustomHostRateLimit:    env.GetFloat("CUSTOM_FETCH_HOST_RATE_LIMIT", 120.0),
		CustomPositiveTTL:      env.GetDuration("CUSTOM_FETCH_POSITIVE_TTL", 30*time.Second),

		ListenAddr: env.Get("LISTEN_ADDR", ":8080"),
	}
}
