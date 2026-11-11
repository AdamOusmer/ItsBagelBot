// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package rpc

import (
	"ItsBagelBot/app/db/admin/ent"
	domainrpc "ItsBagelBot/internal/domain/rpc"
	usersrpc "ItsBagelBot/internal/domain/rpc/users"
	"ItsBagelBot/pkg/bus"
	dbgate "ItsBagelBot/pkg/db"
	"context"
	"go.uber.org/zap"
)

type adminAuthRPC struct {
	staffGate
	log *zap.Logger
}

func SubscribeAdminAuth(w Wiring, db *ent.Client, authPrefix, auditPrefix string) error {
	a := &adminAuthRPC{staffGate: staffGate{db: db}, log: w.Log}

	staff := w.Within(adminBudget)
	if err := bus.ServeVerbs(staff, authPrefix,
		bus.At("check", a.check),
		bus.At("list", a.listStaff),
		bus.At("upsert", a.upsertStaff),
		bus.At("remove", a.removeStaff),
	); err != nil {
		return err
	}
	return bus.ServeVerbs(staff, auditPrefix,
		bus.At("append", a.auditAppend),
		bus.At("list", a.auditList),
	)
}

func authError(r domainrpc.Refusal) usersrpc.AuthReply { return usersrpc.AuthReply{Refusal: r} }

func (a *adminAuthRPC) check(ctx context.Context, req usersrpc.AuthRequest) usersrpc.AuthReply {
	id, err := parseID(req.UserID)
	if err != nil {
		return authError(refusal(err))
	}
	row, err := a.findStaff(ctx, id)
	if ent.IsNotFound(err) {
		return usersrpc.AuthReply{Admin: false}
	}
	if err != nil {
		return authError(refusal(err))
	}
	if !row.Active {
		return usersrpc.AuthReply{Admin: false}
	}

	row = a.refreshIdentity(ctx, row, req)
	return usersrpc.AuthReply{
		Admin:       true,
		Role:        string(row.Role),
		Login:       row.Login,
		DisplayName: row.DisplayName,
	}
}

func staleIdentity(row *ent.AdminUser, req usersrpc.AuthRequest) bool {
	if req.Login == "" {
		return false
	}
	if req.Login != row.Login {
		return true
	}
	return req.DisplayName != "" && req.DisplayName != row.DisplayName
}

func (a *adminAuthRPC) refreshIdentity(ctx context.Context, row *ent.AdminUser, req usersrpc.AuthRequest) *ent.AdminUser {
	if !staleIdentity(row, req) {
		return row
	}
	upd := a.db.AdminUser.UpdateOneID(row.ID).SetLogin(req.Login)
	if req.DisplayName != "" {
		upd = upd.SetDisplayName(req.DisplayName)
	}
	saved, err := dbgate.WithQuery(ctx, func(ctx context.Context) (*ent.AdminUser, error) {
		return upd.Save(ctx)
	})
	if err != nil {
		return row
	}
	return saved
}

func parseID(s string) (uint64, error) { return bus.UserID(s) }

func SubscribeActiveStaff(w Wiring, client *ent.Client, subject string) error {
	a := &adminAuthRPC{staffGate: staffGate{db: client}, log: w.Log}
	return bus.Serve(w.Within(adminBudget), subject, a.listActiveStaff)
}
