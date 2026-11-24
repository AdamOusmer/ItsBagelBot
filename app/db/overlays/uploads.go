// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package main

import (
	"github.com/nats-io/nats.go"
	valkey_go "github.com/valkey-io/valkey-go"

	"ItsBagelBot/app/db/overlays/assets"
	"ItsBagelBot/app/db/overlays/repository"
	"ItsBagelBot/app/db/overlays/rpc"
	"ItsBagelBot/pkg/env"
	"ItsBagelBot/pkg/health"
	"ItsBagelBot/pkg/objectstore"
	"ItsBagelBot/pkg/ratelimit"
	"ItsBagelBot/pkg/svcboot"
)

// shared is what the asset and feed services use from one process-wide set of clients.
type shared struct {
	rpc    *nats.Conn
	alphas *rpc.AlphaAccess
	valkey valkey_go.Client
}

func newAssets(repo *repository.Overlays, objects objectstore.Store, in shared) *assets.Service {
	return &assets.Service{
		Repo:      repo,
		Objects:   objects,
		Alphas:    in.alphas,
		Allow:     ratelimit.New(in.valkey).Allow,
		Signer:    assets.Signer{Secret: []byte(env.MustGet("OVERLAY_UPLOAD_SECRET"))},
		PublicURL: env.MustGet("R2_PUBLIC_URL"),
	}
}

func serveUploads(core svcboot.Core, service *assets.Service) {
	stopped := health.ServeHandler(env.Get("UPLOAD_LISTEN_ADDR", ":8081"), service.Handler())
	go func() { svcboot.FatalIf(core.Log, <-stopped, "upload server stopped") }()
}
