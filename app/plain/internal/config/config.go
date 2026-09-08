// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

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
	"ItsBagelBot/pkg/env/conf"
	"ItsBagelBot/pkg/svcboot"
)

type Config struct {
	// Infra is the shared NATS/Valkey/listen block (see svcboot.Infra); its
	// fields are promoted, so cfg.NATSURL and cfg.ListenAddr read unchanged.
	svcboot.Infra

	// Lanes and Projection are the blocks this worker shares with sesame; both
	// are embedded so their fields stay promoted (cfg.PremiumSubject). They
	// were a field-for-field copy of sesame's config until the projection
	// modules subject drifted to a second default behind the same variable
	// name.
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
