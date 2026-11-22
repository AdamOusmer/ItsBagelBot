// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package main

import (
	"github.com/nats-io/nats.go"

	"ItsBagelBot/app/db/overlays/assets"
	"ItsBagelBot/app/db/overlays/repository"
	"ItsBagelBot/app/db/overlays/rpc"
	"ItsBagelBot/pkg/env"
	"ItsBagelBot/pkg/health"
	"ItsBagelBot/pkg/objectstore"
	"ItsBagelBot/pkg/svcboot"
)

func newAssets(core svcboot.Core, repo *repository.Overlays, objects objectstore.Store, nc *nats.Conn) *assets.Service {
	return &assets.Service{
		Repo:      repo,
		Objects:   objects,
		Plans:     rpc.VoicePlans{NC: nc},
		Signer:    assets.Signer{Secret: []byte(env.MustGet("OVERLAY_UPLOAD_SECRET"))},
		PublicURL: env.MustGet("R2_PUBLIC_URL"),
	}
}

func serveUploads(core svcboot.Core, service *assets.Service) {
	stopped := health.ServeHandler(env.Get("UPLOAD_LISTEN_ADDR", ":8081"), service.Handler())
	go func() { svcboot.FatalIf(core.Log, <-stopped, "upload server stopped") }()
}
