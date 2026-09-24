// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package main

import (
	"context"
	"time"

	"go.uber.org/zap"

	notificationsrpc "ItsBagelBot/internal/domain/rpc/notifications"
	"ItsBagelBot/pkg/bus"
	"ItsBagelBot/pkg/env"
)

func runCleanup(ctx context.Context, log *zap.Logger) error {
	natsURL := env.Get("NATS_URL", "nats://127.0.0.1:4222")

	nc, err := bus.Connect(bus.RPCURL(natsURL), serviceName+"-cleanup")
	if err != nil {
		return err
	}
	defer nc.Close()

	subject := env.Get("NATS_NOTIFICATIONS_CLEANUP_SUBJECT", "bagel.rpc.internal.notifications.cleanup")

	reply, err := bus.RequestJSONTimeout[notificationsrpc.CleanupReply](
		ctx, nc, subject, notificationsrpc.CleanupRequest{}, 30*time.Second)
	if err != nil {
		return err
	}

	log.Info("notification cleanup done", zap.Int("deleted", reply.Deleted))
	return nil
}
