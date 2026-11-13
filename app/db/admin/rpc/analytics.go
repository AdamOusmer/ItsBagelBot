// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package rpc

import (
	"context"

	"ItsBagelBot/app/db/admin/ent"
	"ItsBagelBot/app/db/admin/ent/adminuser"
	"ItsBagelBot/app/db/admin/repository"
	"ItsBagelBot/internal/analytics"
	domainrpc "ItsBagelBot/internal/domain/rpc"
	usersrpc "ItsBagelBot/internal/domain/rpc/users"
	"ItsBagelBot/pkg/bus"
)

type adminRPC struct {
	repo *repository.Admin
	gate staffGate
	live *analytics.Reader
}

func SubscribeAnalytics(w Wiring, client *ent.Client, subject string) error {
	a := &adminRPC{repo: w.Repo, gate: staffGate{db: client}, live: w.Analytics}
	return bus.Serve(w.Within(adminBudget), subject, a.guardedAnalytics)
}

func (a *adminRPC) guardedAnalytics(ctx context.Context, req usersrpc.AdminRequest) usersrpc.AdminReply {
	actor, errMsg := a.gate.resolveActiveActor(ctx, req.ActorID)
	if errMsg != "" {
		return adminError(domainrpc.Refused(domainrpc.CodeForbidden, errMsg))
	}
	if rank(actor.Role) < rank(adminuser.RoleAdmin) {
		return adminError(domainrpc.Refused(domainrpc.CodeForbidden, "forbidden: admin role or higher required"))
	}
	return a.analytics(ctx, req)
}

func adminError(r domainrpc.Refusal) usersrpc.AdminReply { return usersrpc.AdminReply{Refusal: r} }
