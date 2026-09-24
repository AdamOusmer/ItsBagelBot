// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package databoot

import (
	"context"
	"database/sql"

	entsql "entgo.io/ent/dialect/sql"
	"go.uber.org/zap"

	"ItsBagelBot/pkg/db"
	"ItsBagelBot/pkg/env"
	"ItsBagelBot/pkg/health"
	"ItsBagelBot/pkg/svcboot"
)

func MustEntDriver(core svcboot.Core, defaultSchema string) *entsql.Driver {
	driver, err := db.NewDriver(db.Config{
		Address:  env.Get("DB_ADDR", "127.0.0.1:3306"),
		Username: env.MustGet("DB_USER"),
		Password: env.MustGet("DB_PASS"),
		Schema:   env.Get("DB_SCHEMA", defaultSchema),
		Monitor:  core.NR,
	})
	svcboot.FatalIf(core.Log, err, "failed to open database")
	return driver
}

func AutoMigrate(ctx context.Context, log *zap.Logger, create func(context.Context) error) {
	if !env.GetBool("DB_AUTO_MIGRATE", true) {
		return
	}
	svcboot.FatalIf(log, create(ctx), "failed to run migrations")
}

type Health struct {
	svcboot.Health
	Pool *sql.DB
}

func ServeHealth(d Health, extra ...health.Check) {
	checks := make([]health.Check, 0, len(extra)+1)
	checks = append(checks, extra...)
	checks = append(checks, health.Degrades(db.HealthCheck("mysql", d.Pool)))
	svcboot.ServeHealth(d.Health, checks...)
}
