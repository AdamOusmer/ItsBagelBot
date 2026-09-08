// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// Package databoot is the data-tier half of the svcboot facade: the ent driver,
// the auto-migration step and the health surface the six services behind
// health.itsbagelbot.com/db share.
//
// It is a separate package from pkg/svcboot rather than three more functions in
// it because importing it links a concrete SQL driver. internal/buildguard's
// TestSesameIsReadOnlyToData fails the build if app/twitch/sesame links one:
// sesame is a read-only consumer of the projection, and a boot facade that
// dragged pkg/db into every service would have handed it a write path nothing
// else in the build would have flagged. Splitting here keeps the eight
// non-data mains on pkg/svcboot alone.
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

// MustEntDriver opens the MySQL driver from the fleet's env conventions
// (DB_ADDR, DB_USER, DB_PASS, DB_SCHEMA). Fatal on failure: a data service
// without its database can only crashloop later anyway.
func MustEntDriver(log *zap.Logger, defaultSchema string) *entsql.Driver {
	driver, err := db.NewDriver(db.Config{
		Address:  env.Get("DB_ADDR", "127.0.0.1:3306"),
		Username: env.MustGet("DB_USER"),
		Password: env.MustGet("DB_PASS"),
		Schema:   env.Get("DB_SCHEMA", defaultSchema),
	})
	svcboot.FatalIf(log, err, "failed to open database")
	return driver
}

// AutoMigrate runs the service's ent auto-migration unless disabled by
// DB_AUTO_MIGRATE. The generated ent clients are distinct types per service,
// so the schema-create step arrives as a closure (client.Schema.Create).
func AutoMigrate(ctx context.Context, log *zap.Logger, create func(context.Context) error) {
	if !env.GetBool("DB_AUTO_MIGRATE", true) {
		return
	}
	svcboot.FatalIf(log, create(ctx), "failed to run migrations")
}

// Health is a data-tier service's identity plus the pool its mysql check
// probes. It is svcboot.Health with the database the data tier all share.
type Health struct {
	svcboot.Health
	Pool *sql.DB
}

// ServeHealth is svcboot.ServeHealth with the data tier's own extra check.
// Every service behind health.itsbagelbot.com/db calls this, so the six of them
// cannot drift into reporting different things.
//
// The mysql check sits alongside nats because PingContext exercises the same
// pool the repository code uses, catching a wedged pool or rotated-out
// credentials that IsConnected alone would miss (pkg/db/health.go). It degrades
// rather than fails readiness: a hard failure would pull every pod of a service
// out of rotation on one shared DB blip, turning a brief outage into a total
// one. A healthy ping lands in single-digit ms (measured ~3.6ms pod-to-MySQL
// RTT); much higher means the pool went cold and is paying the ~18ms handshake
// instead of reusing a connection.
//
// The database check stays with each service rather than being hoisted into the
// projector's /db aggregate: the schemas are expected to split across servers,
// and one hoisted check could not name which one went.
//
// extra carries whatever else a given service depends on — a lane check for the
// ones that consume a durable group, nothing for the request/reply-only ones.
// It is a parameter rather than a nil-able subscriber because bus.LaneCheck on a
// nil Subscriber silently passes, which is worse than having no check at all.
func ServeHealth(d Health, extra ...health.Check) {
	checks := make([]health.Check, 0, len(extra)+1)
	checks = append(checks, extra...)
	checks = append(checks, health.Degrades(db.HealthCheck("mysql", d.Pool)))
	svcboot.ServeHealth(d.Health, checks...)
}
