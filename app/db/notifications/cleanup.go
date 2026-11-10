// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package main

import (
	"context"
	"time"

	"go.uber.org/zap"

	notificationsrpc "ItsBagelBot/internal/domain/rpc/notifications"
	"ItsBagelBot/pkg/bus"
	"ItsBagelBot/pkg/env/conf"
	"ItsBagelBot/pkg/svcboot"
)

func runCleanup(ctx context.Context, log *zap.Logger) error {
	nc, err := bus.Connect(bus.RPCURL(svcboot.LoadInfra().NATSURL), serviceName+"-cleanup")
	if err != nil {
		return err
	}
	defer nc.Close()

	subject := conf.NotificationsCleanup.Get()

	for {
		reply, err := bus.RequestJSONTimeout[notificationsrpc.CleanupReply](
			ctx, nc, subject, notificationsrpc.CleanupRequest{}, 30*time.Second)
		if err != nil {
			return err
		}

		log.Info("notification cleanup done", zap.Int("deleted", reply.Deleted))
		if !reply.HasMore {
			return nil
		}
	}
}
