// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package main

import (
	"context"
	"time"

	"ItsBagelBot/app/twitch/outgress/internal/channels"
	"ItsBagelBot/app/twitch/outgress/internal/worker"
	"ItsBagelBot/pkg/bus"
	"ItsBagelBot/pkg/ratelimit"
	"ItsBagelBot/pkg/svcboot"
)

func (d *deps) newSendingCoordination(ctx context.Context, registry *channels.Registry) (ratelimit.Manager, worker.BatchStore, func()) {
	js, closeJS, err := bus.OpenCoordination(d.cfg.NATSURL)
	svcboot.FatalIf(d.log, err, "failed to connect sending coordination")
	setupCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	rate, err := bus.CoordinationBucket(setupCtx, js, "outgress_rate", 10*time.Minute)
	svcboot.FatalIf(d.log, err, "failed to provision rate coordination")
	batch, err := bus.CoordinationBucket(setupCtx, js, "outgress_batch", 2*time.Minute)
	svcboot.FatalIf(d.log, err, "failed to provision batch coordination")
	pause, err := bus.CoordinationBucket(setupCtx, js, "outgress_pause", 0)
	svcboot.FatalIf(d.log, err, "failed to provision pause coordination")
	svcboot.FatalIf(d.log, registry.UseDurablePause(setupCtx, pause), "failed to initialize durable pause")
	d.log.Info("sending coordination ready: NATS quorum; Valkey is not the send authority")
	return ratelimit.NewJetStreamManager(rate), worker.NewJetStreamBatchStore(batch), closeJS
}
