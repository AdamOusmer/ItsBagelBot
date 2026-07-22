package rpc

import (
	"context"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/newrelic/go-agent/v3/newrelic"
	"go.uber.org/zap"

	"ItsBagelBot/app/notifications/repository"
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
func SubscribeMaintenance(nc *nats.Conn, repo *repository.Notifications, subject, queueGroup string, app *newrelic.Application, log *zap.Logger) error {
	m := &maintenanceRPC{repo: repo, log: log}
	return bus.QueueSubscribeJSON[notificationsrpc.CleanupRequest, notificationsrpc.CleanupReply](
		nc, subject, queueGroup, 30*time.Second, app, log, m.cleanup)
}

func (m *maintenanceRPC) cleanup(ctx context.Context, _ notificationsrpc.CleanupRequest) notificationsrpc.CleanupReply {
	log := monitor.TxnLogger(ctx, m.log)
	deleted, err := m.repo.DeleteExpired(ctx, time.Now())
	if err != nil {
		log.Warn("notification cleanup failed", zap.Error(err))
		return notificationsrpc.CleanupReply{Error: err.Error()}
	}
	if deleted > 0 {
		log.Info("notification cleanup swept expired rows", zap.Int("deleted", deleted))
	}
	return notificationsrpc.CleanupReply{Deleted: deleted}
}
