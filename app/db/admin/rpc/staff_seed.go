// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package rpc

import (
	"ItsBagelBot/app/db/admin/ent"
	"ItsBagelBot/app/db/admin/ent/adminuser"
	dbgate "ItsBagelBot/pkg/db"
	"context"
	"fmt"
	"go.uber.org/zap"
)

type StaffSeed struct {
	Owners []uint64
	Admins []uint64
}

func SeedStaff(ctx context.Context, db *ent.Client, seed StaffSeed, log *zap.Logger) error {
	s := staffSeeder{db: db, log: log}
	if err := s.seedRole(ctx, seed.Owners, adminuser.RoleOwner); err != nil {
		return err
	}
	return s.seedRole(ctx, seed.Admins, adminuser.RoleAdmin)
}

type staffSeeder struct {
	db  *ent.Client
	log *zap.Logger
}

func (s staffSeeder) seedRole(ctx context.Context, ids []uint64, role adminuser.Role) error {
	for _, id := range ids {
		if err := s.seedOne(ctx, id, role); err != nil {
			return err
		}
	}
	return nil
}

func (s staffSeeder) seedOne(ctx context.Context, id uint64, role adminuser.Role) error {
	existing, err := dbgate.WithQuery(ctx, func(ctx context.Context) (*ent.AdminUser, error) {
		return s.db.AdminUser.Query().Where(adminuser.IDEQ(id)).Only(ctx)
	})
	if err == nil {
		upd := s.db.AdminUser.UpdateOneID(id).SetActive(true)
		if rank(role) > rank(existing.Role) {
			upd = upd.SetRole(role)
		}
		return dbgate.WithExec(ctx, func(ctx context.Context) error {
			return upd.Exec(ctx)
		})
	}
	if !ent.IsNotFound(err) {
		return err
	}
	login := fmt.Sprintf("bootstrap-%d", id)
	if err := upsertStaffRow(ctx, s.db, staffRow{id: id, login: login, display: login, role: role}); err != nil {
		return err
	}
	s.log.Info("seeded bootstrap staff", zap.Uint64("id", id), zap.String("role", string(role)))
	return nil
}
