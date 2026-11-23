// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package main

import (
	"context"
	"errors"

	"ItsBagelBot/app/db/overlays/livefeed"
	"ItsBagelBot/app/db/overlays/repository"
	"ItsBagelBot/pkg/ratelimit"
	"ItsBagelBot/pkg/svcboot"
)

type unwiredFrames struct{}

func (unwiredFrames) Publish(context.Context, string, string, []byte) (uint64, error) {
	return 0, errors.New("overlay feed publisher is not wired to bus.Overlays")
}

func newFeed(core svcboot.Core, repo *repository.Overlays) *livefeed.Service {
	client := svcboot.MustValkey(core)
	return &livefeed.Service{Client: client, Frames: unwiredFrames{}, Channels: repo, Allow: ratelimit.New(client).Allow, Log: core.Log}
}
