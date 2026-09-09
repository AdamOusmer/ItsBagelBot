// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package rpc

import (
	"context"
	"time"

	"go.uber.org/zap"

	"ItsBagelBot/app/db/notifications/repository"
	notificationsrpc "ItsBagelBot/internal/domain/rpc/notifications"
	"ItsBagelBot/pkg/bus"
	"ItsBagelBot/pkg/monitor"
)

type maintenanceRPC struct {
	repo *repository.Notifications
	log  *zap.Logger
}

// SubscribeMaintenance registers the internal janitor verb the k3s cron drives.
// The subject is NOT exported from the NOTIFICATIONS_RPC account, so only a
// client holding the notifications credentials (the cron reuses them) can reach
// it. The queue group means exactly one replica runs the sweep per cron tick.
func SubscribeMaintenance(w Wiring, subject string) error {
	m := &maintenanceRPC{repo: w.Repo, log: w.Log}
	return bus.Serve(w.Within(cleanupBudget), subject, m.cleanup)
}

func (m *maintenanceRPC) cleanup(ctx context.Context, _ notificationsrpc.CleanupRequest) notificationsrpc.CleanupReply {
	log := monitor.TxnLogger(ctx, m.log)
	deleted, err := m.repo.DeleteExpired(ctx, time.Now())
	if err != nil {
		log.Warn("notification cleanup failed", zap.Error(err))
		return notificationsrpc.CleanupReply{Refusal: bus.Classify(err)}
	}
	if deleted > 0 {
		log.Info("notification cleanup swept expired rows", zap.Int("deleted", deleted))
	}
	return notificationsrpc.CleanupReply{Deleted: deleted}
}
