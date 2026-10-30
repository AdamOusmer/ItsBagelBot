// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package main

import (
	"context"
	"time"

	"github.com/nats-io/nkeys"
	"go.uber.org/zap"
	"k8s.io/client-go/rest"

	"ItsBagelBot/app/clusterops/internal/aclpush"
	"ItsBagelBot/app/clusterops/internal/certroll"
	"ItsBagelBot/app/clusterops/internal/natsclaims"
	"ItsBagelBot/pkg/env"
	"ItsBagelBot/pkg/health"
	"ItsBagelBot/pkg/svcboot"
)

const (
	serviceName       = "clusterops"
	certRollNamespace = "app"
	syncTimeout       = 30 * time.Second
)

type claimsSync struct {
	log          *zap.Logger
	pusher       aclpush.ClaimsPusher
	signer       nkeys.KeyPair
	accountsFile string
	keysFile     string
}

func main() {
	core, done := svcboot.NewCore(serviceName)
	defer done()
	log, ctx := core.Log, core.Ctx

	kube, err := rest.InClusterConfig()
	svcboot.FatalIf(log, err, "failed to load in-cluster kube config")
	roller, err := certroll.New(kube, log)
	svcboot.FatalIf(log, err, "failed to build cert roller")
	go roller.Run(ctx, []string{certRollNamespace}, certroll.Debounce)

	signer, err := aclpush.Signer(env.MustGet("NATS_SIGNING_SEED"))
	svcboot.FatalIf(log, err, "failed to load operator signing key")
	claims, err := natsclaims.New(natsclaims.Config{
		HubURL: env.MustGet("NATS_HUB_URL"), LeafURL: env.MustGet("NATS_LEAF_URL"),
		SysJWT: env.MustGet("NATS_SYS_JWT"), SysSeed: env.MustGet("NATS_SYS_NKEY_SEED"),
	})
	svcboot.FatalIf(log, err, "failed to connect nats claims adapter")
	defer claims.Close()

	s := claimsSync{
		log: log, pusher: claims, signer: signer,
		accountsFile: env.MustGet("NATS_ACCOUNTS_FILE"), keysFile: env.MustGet("NATS_ACCOUNTS_KEYS_FILE"),
	}
	go s.run(ctx, env.GetDuration("CLAIMS_SYNC_INTERVAL", time.Minute))

	health.Serve(core.ListenAddr, serviceName)
	log.Info("clusterops ready")
	<-ctx.Done()
}

func (s claimsSync) run(ctx context.Context, every time.Duration) {
	tick := time.NewTicker(every)
	defer tick.Stop()
	for {
		s.once(ctx)
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
	}
}

func (s claimsSync) once(ctx context.Context) {
	ctx, cancel := context.WithTimeout(ctx, syncTimeout)
	defer cancel()
	compiled, err := aclpush.Load(s.accountsFile, s.keysFile)
	if err != nil {
		s.log.Error("claims sync: load accounts", zap.Error(err))
		return
	}
	pushed, err := aclpush.Sync(ctx, s.pusher, s.signer, compiled)
	for _, r := range pushed {
		s.log.Info("claims sync: pushed", zap.String("account", r.Account), zap.String("cluster", string(r.Cluster)))
	}
	if err != nil {
		s.log.Error("claims sync failed", zap.Error(err))
	}
}
