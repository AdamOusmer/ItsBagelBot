// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package main

import (
	"context"

	"github.com/nats-io/nats.go"

	"ItsBagelBot/app/db/overlays/livefeed"
	"ItsBagelBot/app/db/overlays/repository"
	"ItsBagelBot/app/db/overlays/rpc"
	"ItsBagelBot/pkg/bus"
	"ItsBagelBot/pkg/ratelimit"
	"ItsBagelBot/pkg/svcboot"
)

type coreState struct{ nc *nats.Conn }

func (s coreState) Publish(ctx context.Context, subject string, payload []byte) error {
	return bus.PublishCore(ctx, s.nc, subject, payload)
}

func newFeed(core svcboot.Core, repo *repository.Overlays, rpcConn *nats.Conn) (*livefeed.Service, *nats.Conn) {
	client := svcboot.MustValkey(core)
	feedConn, err := bus.ConnectOverlayFeed(core.NATSURL)
	svcboot.FatalIf(core.Log, err, "failed to connect overlay feed bus")
	alerts, err := bus.NewOverlayPublisher(feedConn)
	svcboot.FatalIf(core.Log, err, "failed to open overlay alert publisher")
	return &livefeed.Service{
		Client: client, Alerts: alerts, State: coreState{nc: feedConn}, Plans: rpc.VoicePlans{NC: rpcConn}, Alphas: rpc.AlphaAccess{NC: rpcConn},
		Channels: repo, Allow: ratelimit.New(client).Allow, Log: core.Log,
	}, feedConn
}
