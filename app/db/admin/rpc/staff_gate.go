// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package rpc

import (
	"ItsBagelBot/app/db/admin/ent"
	"ItsBagelBot/app/db/admin/ent/adminuser"
	dbgate "ItsBagelBot/pkg/db"
	"context"
)

type staffGate struct {
	db *ent.Client
}

func rank(r adminuser.Role) int {
	switch r {
	case adminuser.RoleOwner:
		return 3
	case adminuser.RoleAdmin:
		return 2
	case adminuser.RoleModerator:
		return 1
	default:
		return 0
	}
}

func isManager(r adminuser.Role) bool { return r == adminuser.RoleAdmin || r == adminuser.RoleOwner }

func (g staffGate) findStaff(ctx context.Context, id uint64) (*ent.AdminUser, error) {
	return dbgate.WithQuery(ctx, func(ctx context.Context) (*ent.AdminUser, error) {
		return g.db.AdminUser.Query().Where(adminuser.IDEQ(id)).Only(ctx)
	})
}

func (g staffGate) resolveActiveActor(ctx context.Context, rawID string) (*ent.AdminUser, string) {
	actorID, errMsg := parseActorID(rawID)
	if errMsg != "" {
		return nil, errMsg
	}
	actor, err := g.findStaff(ctx, actorID)
	if err != nil && !ent.IsNotFound(err) {
		return nil, err.Error()
	}
	return activeActor(actor)
}

func parseActorID(rawID string) (uint64, string) {
	actorID, err := parseID(rawID)
	if err != nil {
		if rawID == "" {
			return 0, "actor_id required"
		}
		return 0, "actor_id must be numeric"
	}
	return actorID, ""
}

func activeActor(actor *ent.AdminUser) (*ent.AdminUser, string) {
	if actor == nil || !actor.Active {
		return nil, "forbidden: actor is not active staff"
	}
	return actor, ""
}

func activeActorOf(rows map[uint64]*ent.AdminUser, rawID string) (*ent.AdminUser, string) {
	actorID, errMsg := parseActorID(rawID)
	if errMsg != "" {
		return nil, errMsg
	}
	return activeActor(rows[actorID])
}
