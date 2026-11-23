// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package main

import (
	"ItsBagelBot/app/db/overlays/livefeed"
	"ItsBagelBot/app/db/overlays/repository"
	"ItsBagelBot/pkg/bus"
	"ItsBagelBot/pkg/ratelimit"
	"ItsBagelBot/pkg/svcboot"
)

// newFeed connects the feed's own Valkey and its OVERLAY_FEED writer connection.
func newFeed(core svcboot.Core, repo *repository.Overlays) *livefeed.Service {
	client := svcboot.MustValkey(core)
	nc, err := bus.ConnectOverlayFeed(core.NATSURL)
	svcboot.FatalIf(core.Log, err, "failed to connect overlay feed writer")
	frames, err := bus.OpenOverlayFeed(core.Ctx, nc)
	svcboot.FatalIf(core.Log, err, "failed to open overlay feed stream")
	return &livefeed.Service{Client: client, Frames: frames, Channels: repo, Allow: ratelimit.New(client).Allow, Log: core.Log}
}
