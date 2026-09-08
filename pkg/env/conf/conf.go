// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// Package conf holds the environment blocks that more than one service loads
// identically, so a shared knob has exactly one name and one default.
//
// It exists because copying a config struct field-for-field is how
// NATS_INTERNAL_PROJECTION_MODULES_SUBJECT ended up with two different
// defaults behind one variable name: sesame pointed it at the projector's
// dashboard verb while plain, projector and app/db/modules pointed it at the
// internal projection verb. Setting that variable in Doppler would have
// broken whichever service disagreed with the value. See Projection.
//
// Blocks are embedded, not referenced through a named field, so a service's
// existing cfg.PremiumSubject spelling keeps working; that is also why the
// field names carry their own prefix instead of reading Lanes.Premium.
package conf

import "ItsBagelBot/pkg/env"

// Lanes is the pipeline lane block: the two ingress lanes a pipeline service
// drains, laned by broadcaster status, and the two outgress lanes it publishes
// the resulting actions onto.
type Lanes struct {
	PremiumSubject  string
	StandardSubject string

	OutgressPremiumSubject  string
	OutgressStandardSubject string
}

// LoadLanes reads the lane block from the environment.
func LoadLanes() Lanes {
	return Lanes{
		PremiumSubject:  env.Get("NATS_INGRESS_PREMIUM_SUBJECT", "twitch.ingress.event.premium"),
		StandardSubject: env.Get("NATS_INGRESS_STANDARD_SUBJECT", "twitch.ingress.event.standard"),

		OutgressPremiumSubject:  env.Get("NATS_OUTGRESS_PREMIUM_SUBJECT", "twitch.outgress.premium"),
		OutgressStandardSubject: env.Get("NATS_OUTGRESS_STANDARD_SUBJECT", "twitch.outgress.standard"),
	}
}

// Projection is the cold-key fallback block behind the Valkey settings
// projection: the RPC subject asked when a projection read misses, plus the
// prefix a service subscribes to for push invalidation of its in-process copy.
type Projection struct {
	ProjectionUsersSubject    string
	ProjectionModulesSubject  string
	ProjectionCommandsSubject string

	CacheInvalidationPrefix string
}

// LoadProjection reads the internal projection get verbs — the subjects
// app/db/{users,modules,commands} serve and the projector mirrors.
func LoadProjection() Projection {
	return Projection{
		ProjectionUsersSubject:    env.Get("NATS_INTERNAL_PROJECTION_USERS_SUBJECT", "bagel.rpc.internal.projection.users.get"),
		ProjectionModulesSubject:  env.Get("NATS_INTERNAL_PROJECTION_MODULES_SUBJECT", "bagel.rpc.internal.projection.modules.get"),
		ProjectionCommandsSubject: env.Get("NATS_INTERNAL_PROJECTION_COMMANDS_SUBJECT", "bagel.rpc.internal.projection.commands.get"),

		CacheInvalidationPrefix: env.Get("NATS_CACHE_INVALIDATION_PREFIX", "bagel.cache.invalidate"),
	}
}

// LoadProjectionViaProjector re-points modules and commands at the PROJECTOR's
// dashboard get verbs. The projector owns Valkey, so its miss path hydrates the
// projection and the next read is a plain Valkey hit; a service on the hot path
// (sesame) wants that, and never asks the modules/commands services directly.
// Users keeps the internal subject: the projector exposes no user-shaped read.
//
// This is a decorator over LoadProjection rather than a second full loader on
// purpose. The two used to be independent copies keyed on the SAME variable
// names with different defaults, which meant the drift was invisible until
// someone set one of them; now the dashboard verbs have their own variables
// and default off the projector's own prefix, so re-pointing the projector
// moves both sides at once.
func LoadProjectionViaProjector() Projection {
	p := LoadProjection()
	prefix := env.Get("NATS_PROJECTOR_DASHBOARD_SUBJECT_PREFIX", "bagel.rpc.projector.dashboard")
	p.ProjectionModulesSubject = env.Get("NATS_PROJECTOR_DASHBOARD_MODULES_SUBJECT", prefix+".modules.get")
	p.ProjectionCommandsSubject = env.Get("NATS_PROJECTOR_DASHBOARD_COMMANDS_SUBJECT", prefix+".commands.get")
	return p
}
