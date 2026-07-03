package main

import (
	"context"
	"time"

	"go.uber.org/zap"

	notificationsrpc "ItsBagelBot/internal/domain/rpc/notifications"
	"ItsBagelBot/pkg/bus"
	"ItsBagelBot/pkg/env"
)

// runCleanup is the one-shot cron entrypoint (`notifications cleanup`). It dials
// NATS with the service's own RPC credentials and fires the internal cleanup
// verb — a request/reply so the job surfaces the swept count and exits non-zero
// on failure — then returns. Reusing the service image + creds means no extra
// NATS account or image to maintain just for the janitor.
func runCleanup(ctx context.Context, log *zap.Logger) error {
	natsURL := env.Get("NATS_URL", "nats://127.0.0.1:4222")

	nc, err := bus.Connect(bus.RPCURL(natsURL), serviceName+"-cleanup")
	if err != nil {
		return err
	}
	defer nc.Close()

	subject := env.Get("NATS_NOTIFICATIONS_CLEANUP_SUBJECT", "bagel.rpc.internal.notifications.cleanup")

	// RequestJSONTimeout already maps a {"error": ...} reply into a Go error, so
	// a nil error here means the sweep succeeded.
	reply, err := bus.RequestJSONTimeout[notificationsrpc.CleanupReply](
		ctx, nc, subject, notificationsrpc.CleanupRequest{}, 30*time.Second)
	if err != nil {
		return err
	}

	log.Info("notification cleanup done", zap.Int("deleted", reply.Deleted))
	return nil
}
