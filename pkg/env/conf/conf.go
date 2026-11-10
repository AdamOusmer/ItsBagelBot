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
		PremiumSubject:  IngressPremiumSubject.Get(),
		StandardSubject: IngressStandardSubject.Get(),

		OutgressPremiumSubject:  OutgressPremiumSubject.Get(),
		OutgressStandardSubject: OutgressStandardSubject.Get(),
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
		ProjectionUsersSubject:    ProjectionUsersSubject.Get(),
		ProjectionModulesSubject:  ProjectionModulesSubject.Get(),
		ProjectionCommandsSubject: ProjectionCommandsSubject.Get(),

		CacheInvalidationPrefix: CacheInvalidationPrefix.Get(),
	}
}

func LoadProjectionViaProjector() Projection {
	p := LoadProjection()
	prefix := ProjectorDashboardPrefix.Get()
	p.ProjectionModulesSubject = env.Get("NATS_PROJECTOR_DASHBOARD_MODULES_SUBJECT", prefix+".modules.get")
	p.ProjectionCommandsSubject = env.Get("NATS_PROJECTOR_DASHBOARD_COMMANDS_SUBJECT", prefix+".commands.get")
	return p
}
