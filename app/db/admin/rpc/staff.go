// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package rpc

import (
	"ItsBagelBot/app/db/admin/ent"
	"ItsBagelBot/app/db/admin/ent/adminuser"
	domainrpc "ItsBagelBot/internal/domain/rpc"
	usersrpc "ItsBagelBot/internal/domain/rpc/users"
	dbgate "ItsBagelBot/pkg/db"
	"ItsBagelBot/pkg/monitor"
	"context"
	"go.uber.org/zap"
)

func (a *adminAuthRPC) listStaff(ctx context.Context, _ usersrpc.AuthRequest) usersrpc.AuthReply {
	return a.staffList(ctx, false)
}

func (a *adminAuthRPC) listActiveStaff(ctx context.Context, _ usersrpc.AuthRequest) usersrpc.AuthReply {
	return a.staffList(ctx, true)
}

func (a *adminAuthRPC) staffList(ctx context.Context, active bool) usersrpc.AuthReply {
	rows, err := dbgate.WithQuery(ctx, func(ctx context.Context) ([]*ent.AdminUser, error) {
		q := a.db.AdminUser.Query().Order(ent.Asc(adminuser.FieldCreatedAt))
		if active {
			q = q.Where(adminuser.ActiveEQ(true))
		}
		return q.All(ctx)
	})
	if err != nil {
		return authError(refusal(err))
	}
	out := make([]usersrpc.AdminAcctView, 0, len(rows))
	for _, r := range rows {
		out = append(out, adminViewOf(r))
	}
	return usersrpc.AuthReply{Admins: out}
}

func (a *adminAuthRPC) upsertStaff(ctx context.Context, req usersrpc.AuthRequest) usersrpc.AuthReply {
	log := monitor.TxnLogger(ctx, a.log)
	var id uint64
	var newRole adminuser.Role
	addedBy, _ := parseID(req.ActorID)
	errMsg, err := a.inStaffTx(ctx, func(ctx context.Context, tx *ent.Tx) (string, error) {
		rows, err := lockStaff(ctx, tx, req.ActorID, req.UserID)
		if err != nil {
			return "", err
		}
		var errMsg string
		id, newRole, errMsg = validateUpsert(rows, req)
		if errMsg != "" {
			return errMsg, nil
		}
		display := req.DisplayName
		if display == "" {
			display = req.Login
		}
		return "", writeStaffRow(ctx, tx, rows[id], staffRow{id: id, login: req.Login, display: display, role: newRole, addedBy: addedBy})
	})
	if err != nil {
		return authError(refusal(err))
	}
	if errMsg != "" {
		return authError(domainrpc.Refused(domainrpc.CodeInvalid, errMsg))
	}
	log.Info("staff upsert", zap.Uint64("id", id), zap.String("role", string(newRole)), zap.Uint64("by", addedBy))
	return a.listStaff(ctx, usersrpc.AuthRequest{})
}

func validateUpsert(rows map[uint64]*ent.AdminUser, req usersrpc.AuthRequest) (id uint64, newRole adminuser.Role, errMsg string) {
	actor, errMsg := activeActorOf(rows, req.ActorID)
	if errMsg != "" {
		return 0, "", errMsg
	}
	actorRole := actor.Role
	if !isManager(actorRole) {
		return 0, "", "forbidden: managers only"
	}
	id, err := parseID(req.UserID)
	if err != nil {
		return 0, "", err.Error()
	}
	if req.Login == "" {
		return 0, "", "login required"
	}

	newRole, errMsg = resolveNewRole(req.Role, actorRole)
	if errMsg != "" {
		return 0, "", errMsg
	}
	if actor.ID == id {
		return 0, "", "forbidden: cannot change your own role"
	}
	if errMsg := guardExistingTarget(rows[id], actorRole); errMsg != "" {
		return 0, "", errMsg
	}
	return id, newRole, ""
}

func resolveNewRole(raw string, actorRole adminuser.Role) (adminuser.Role, string) {
	newRole := adminuser.Role(raw)
	if raw == "" {
		newRole = adminuser.RoleModerator
	}
	if err := adminuser.RoleValidator(newRole); err != nil {
		return "", "role must be moderator, admin or owner"
	}
	if newRole == adminuser.RoleOwner && actorRole != adminuser.RoleOwner {
		return "", "forbidden: only an owner can grant owner"
	}
	return newRole, ""
}

func guardExistingTarget(existing *ent.AdminUser, actorRole adminuser.Role) string {
	if existing == nil {
		return ""
	}
	if existing.Role == adminuser.RoleOwner {
		return "forbidden: an owner's role cannot be changed"
	}
	if actorRole == adminuser.RoleAdmin && existing.Role == adminuser.RoleAdmin {
		return "forbidden: admins cannot change another admin"
	}
	return ""
}

func (a *adminAuthRPC) removeStaff(ctx context.Context, req usersrpc.AuthRequest) usersrpc.AuthReply {
	log := monitor.TxnLogger(ctx, a.log)
	var id uint64
	errMsg, err := a.inStaffTx(ctx, func(ctx context.Context, tx *ent.Tx) (string, error) {
		rows, err := lockStaff(ctx, tx, req.ActorID, req.UserID)
		if err != nil {
			return "", err
		}
		var errMsg string
		id, errMsg = validateRemove(rows, req)
		if errMsg == "" && rows[id].Role == adminuser.RoleOwner {
			errMsg, err = guardLastOwner(ctx, tx)
		}
		if errMsg != "" || err != nil {
			return errMsg, err
		}
		return "", tx.AdminUser.UpdateOneID(id).SetActive(false).Exec(ctx)
	})
	if err != nil {
		return authError(refusal(err))
	}
	if errMsg != "" {
		return authError(domainrpc.Refused(domainrpc.CodeInvalid, errMsg))
	}
	actorID, _ := parseID(req.ActorID)
	log.Info("staff removed", zap.Uint64("id", id), zap.Uint64("by", actorID))
	return a.listStaff(ctx, usersrpc.AuthRequest{})
}

func validateRemove(rows map[uint64]*ent.AdminUser, req usersrpc.AuthRequest) (uint64, string) {
	actor, errMsg := activeActorOf(rows, req.ActorID)
	if errMsg != "" {
		return 0, errMsg
	}
	actorRole := actor.Role
	if !isManager(actorRole) {
		return 0, "forbidden: managers only"
	}
	id, err := parseID(req.UserID)
	if err != nil {
		return 0, err.Error()
	}
	if actor.ID == id {
		return 0, "forbidden: cannot remove yourself"
	}

	target := rows[id]
	if target == nil {
		return 0, "staff not found"
	}
	return id, guardTargetRemoval(actorRole, target)
}

func guardTargetRemoval(actorRole adminuser.Role, target *ent.AdminUser) string {
	if actorRole == adminuser.RoleAdmin && target.Role == adminuser.RoleAdmin {
		return "forbidden: admins cannot remove another admin"
	}
	if target.Role == adminuser.RoleOwner && actorRole != adminuser.RoleOwner {
		return "forbidden: cannot remove an owner"
	}
	return ""
}

func guardLastOwner(ctx context.Context, tx *ent.Tx) (string, error) {
	owners, err := tx.AdminUser.Query().
		Where(adminuser.RoleEQ(adminuser.RoleOwner), adminuser.ActiveEQ(true)).
		Count(ctx)
	if err != nil {
		return "", err
	}
	if owners <= 1 {
		return "cannot remove the last owner", nil
	}
	return "", nil
}

func adminViewOf(r *ent.AdminUser) usersrpc.AdminAcctView {
	return usersrpc.AdminAcctView{
		ID:          r.ID,
		Login:       r.Login,
		DisplayName: r.DisplayName,
		Role:        string(r.Role),
		Active:      r.Active,
		AddedBy:     r.AddedBy,
		CreatedAt:   r.CreatedAt,
	}
}
