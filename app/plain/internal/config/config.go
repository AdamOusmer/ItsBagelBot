// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package config

import (
	"time"

	"ItsBagelBot/pkg/env"
	"ItsBagelBot/pkg/env/conf"
	"ItsBagelBot/pkg/svcboot"
)

type Config struct {
	svcboot.Infra

	conf.Lanes
	conf.Projection

	MaxRoutines    int
	MaxConsumers   int
	ScaleUpAfter   time.Duration
	ScaleDownAfter time.Duration
	PremiumReserve int

	SpecialUserIDs string

	LiveTTL time.Duration
}

func Load() *Config {
	return &Config{
		Infra: svcboot.LoadInfra(),

		Lanes:      conf.LoadLanes(),
		Projection: conf.LoadProjection(),

		MaxRoutines:    env.GetInt("WORKER_MAX_ROUTINES", 50),
		MaxConsumers:   env.GetInt("WORKER_MAX_CONSUMERS", 3),
		ScaleUpAfter:   env.GetDuration("WORKER_SCALE_UP_AFTER", 5*time.Second),
		ScaleDownAfter: env.GetDuration("WORKER_SCALE_DOWN_AFTER", 30*time.Second),
		PremiumReserve: env.GetInt("WORKER_PREMIUM_RESERVE_PERCENT", 25),

		SpecialUserIDs: env.Get("TWITCH_SPECIAL_USER_IDS", ""),

		LiveTTL: env.GetDuration("WORKER_LIVE_TTL", 12*time.Hour),
	}
}
