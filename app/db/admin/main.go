// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package main

import (
	"context"
	"strconv"
	"strings"
	"time"

	"ItsBagelBot/app/db/admin/ent"
	_ "ItsBagelBot/app/db/admin/ent/runtime"
	"ItsBagelBot/app/db/admin/repository"
	"ItsBagelBot/app/db/admin/rollup"
	"ItsBagelBot/app/db/admin/rpc"
	"ItsBagelBot/pkg/bus"
	"ItsBagelBot/pkg/env"
	"ItsBagelBot/pkg/svcboot"
	"ItsBagelBot/pkg/svcboot/databoot"

	"go.uber.org/zap"
)

const (
	serviceName = "admin"
	queueGroup  = "admin-rpc"
)

func main() {
	core, done := svcboot.NewCore(serviceName)
	defer done()
	driver := databoot.MustEntDriver(core)
	client := ent.NewClient(ent.Driver(driver))
	defer func() { _ = client.Close() }()
	databoot.AutoMigrate(core.Ctx, core.Log, func(ctx context.Context) error { return client.Schema.Create(ctx) })

	nc := svcboot.MustRPCConn(core, core.NATSRPCURL)
	defer nc.Close()
	vc := svcboot.MustValkey(core)
	repo := repository.New(client)
	w := rpc.Wiring{RPCWiring: bus.RPCWiring{NC: nc, App: core.NR, Queue: queueGroup, Log: core.Log}, Repo: repo}
	subscribe(w, client)
	seedStaff(core, client)
	go rollupAnalytics(core.Ctx, rollup.New(vc, repo), core.Log)

	databoot.ServeHealth(databoot.Health{
		Health: svcboot.Health{Log: core.Log, NC: nc, Service: serviceName, QueueGroup: queueGroup, ListenAddr: core.ListenAddr},
		Pool:   driver.DB(),
	})
	core.Log.Info("admin service ready")
	core.Await()
}

func subscribe(w rpc.Wiring, client *ent.Client) {
	auth := env.Get("NATS_ADMIN_AUTH_SUBJECT_PREFIX", "bagel.rpc.admin.user.auth")
	audit := env.Get("NATS_ADMIN_AUDIT_SUBJECT_PREFIX", "bagel.rpc.admin.user.audit")
	svcboot.FatalIf(w.Log, rpc.SubscribeAdminAuth(w, client, auth, audit), "failed to subscribe staff and audit rpc")
	subject := env.Get("NATS_ADMIN_ANALYTICS_SUBJECT", "bagel.rpc.admin.analytics")
	svcboot.FatalIf(w.Log, rpc.SubscribeAnalytics(w, client, subject), "failed to subscribe analytics rpc")
	active := env.Get("NATS_INTERNAL_ADMIN_ACTIVE_STAFF_SUBJECT", "bagel.rpc.internal.admin.staff.active")
	svcboot.FatalIf(w.Log, rpc.SubscribeActiveStaff(w, client, active), "failed to subscribe active staff rpc")
	delivery := env.Get("NATS_INTERNAL_ADMIN_AUDIT_SUBJECT", "bagel.rpc.internal.admin.audit.append")
	svcboot.FatalIf(w.Log, rpc.SubscribeAuditDelivery(w, client, delivery), "failed to subscribe audit delivery rpc")
}

func seedStaff(core svcboot.Core, client *ent.Client) {
	seed := rpc.StaffSeed{
		Owners: parseIDs(env.Get("OWNER_BOOTSTRAP_IDS", "804932984")),
		Admins: parseIDs(env.Get("ADMIN_BOOTSTRAP_IDS", "")),
	}
	svcboot.FatalIf(core.Log, rpc.SeedStaff(core.Ctx, client, seed, core.Log), "failed to seed bootstrap staff")
}

func parseIDs(raw string) []uint64 {
	var ids []uint64
	for _, item := range strings.Split(raw, ",") {
		if id, err := strconv.ParseUint(strings.TrimSpace(item), 10, 64); err == nil && id > 0 {
			ids = append(ids, id)
		}
	}
	return ids
}

func rollupAnalytics(ctx context.Context, worker *rollup.Rollup, log *zap.Logger) {
	run := func() {
		if count, err := worker.Run(ctx, time.Now()); err != nil {
			log.Error("failed to roll up analytics", zap.Error(err))
		} else if count > 0 {
			log.Info("rolled up analytics", zap.Int("count", count))
		}
	}
	run()
	ticker := time.NewTicker(env.GetDuration("ADMIN_ANALYTICS_ROLLUP_INTERVAL", 15*time.Minute))
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			run()
		}
	}
}
