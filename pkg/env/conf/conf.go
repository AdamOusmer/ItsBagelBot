// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package conf

import "ItsBagelBot/pkg/env"

type Lanes struct {
	PremiumSubject  string
	StandardSubject string

	OutgressPremiumSubject  string
	OutgressStandardSubject string
}

func LoadLanes() Lanes {
	return Lanes{
		PremiumSubject:  env.Get("NATS_INGRESS_PREMIUM_SUBJECT", "twitch.ingress.event.premium"),
		StandardSubject: env.Get("NATS_INGRESS_STANDARD_SUBJECT", "twitch.ingress.event.standard"),

		OutgressPremiumSubject:  env.Get("NATS_OUTGRESS_PREMIUM_SUBJECT", "twitch.outgress.premium"),
		OutgressStandardSubject: env.Get("NATS_OUTGRESS_STANDARD_SUBJECT", "twitch.outgress.standard"),
	}
}

type Projection struct {
	ProjectionUsersSubject    string
	ProjectionModulesSubject  string
	ProjectionCommandsSubject string

	CacheInvalidationPrefix string
}

func LoadProjection() Projection {
	return Projection{
		ProjectionUsersSubject:    env.Get("NATS_INTERNAL_PROJECTION_USERS_SUBJECT", "bagel.rpc.internal.projection.users.get"),
		ProjectionModulesSubject:  env.Get("NATS_INTERNAL_PROJECTION_MODULES_SUBJECT", "bagel.rpc.internal.projection.modules.get"),
		ProjectionCommandsSubject: env.Get("NATS_INTERNAL_PROJECTION_COMMANDS_SUBJECT", "bagel.rpc.internal.projection.commands.get"),

		CacheInvalidationPrefix: env.Get("NATS_CACHE_INVALIDATION_PREFIX", "bagel.cache.invalidate"),
	}
}

func LoadProjectionViaProjector() Projection {
	p := LoadProjection()
	prefix := env.Get("NATS_PROJECTOR_DASHBOARD_SUBJECT_PREFIX", "bagel.rpc.projector.dashboard")
	p.ProjectionModulesSubject = env.Get("NATS_PROJECTOR_DASHBOARD_MODULES_SUBJECT", prefix+".modules.get")
	p.ProjectionCommandsSubject = env.Get("NATS_PROJECTOR_DASHBOARD_COMMANDS_SUBJECT", prefix+".commands.get")
	return p
}
