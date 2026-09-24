// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package main

import (
	"context"
	"net/http"
	"time"

	"go.uber.org/zap"
	"k8s.io/client-go/rest"

	"ItsBagelBot/app/deployer/internal/config"
	"ItsBagelBot/app/deployer/internal/engine"
	"ItsBagelBot/app/deployer/internal/events"
	"ItsBagelBot/app/deployer/internal/gh"
	"ItsBagelBot/app/deployer/internal/kube/apply"
	"ItsBagelBot/app/deployer/internal/kube/watch"
	"ItsBagelBot/app/deployer/internal/ports"
	"ItsBagelBot/app/deployer/internal/registry"
	"ItsBagelBot/app/deployer/internal/rpc"
	"ItsBagelBot/app/deployer/internal/stage"
	"ItsBagelBot/app/deployer/internal/stages/cluster"
	"ItsBagelBot/app/deployer/internal/stages/github"
	"ItsBagelBot/app/deployer/internal/store"
	"ItsBagelBot/pkg/bus"
	"ItsBagelBot/pkg/svcboot"
)

const (
	serviceName = "deployer"
	queueGroup  = "deployer-rpc"
)

const (
	httpTimeout       = 10 * time.Second
	storeSetupTimeout = 30 * time.Second
	// Must fit the 45s terminationGracePeriodSeconds with Await's 15s drain and the 10s preStop.
	engineDrainTimeout = 15 * time.Second
)

func main() {
	core, done := svcboot.NewCore(serviceName)
	defer done()
	log, ctx := core.Log, core.Ctx

	cfg, err := config.Load()
	svcboot.FatalIf(log, err, "failed to load config")

	nc := svcboot.MustRPCConn(core, bus.RPCURL(core.NATSURL))
	defer nc.Close()
	runs, closeJS := openStore(core, cfg)
	defer closeJS()

	kube, err := rest.InClusterConfig()
	svcboot.FatalIf(log, err, "failed to load in-cluster kube config")

	eng := engine.New(engine.Deps{
		Store:    runs,
		Events:   events.New(nc),
		Stages:   append(cluster.All(), github.All()...),
		Stage:    stageDeps(cfg, kube, log),
		Services: cluster.Order(),
		Log:      log,
	})
	stopped := runEngine(ctx, eng, log)

	wiring := bus.RPCWiring{NC: nc, App: core.NR, Queue: queueGroup, Log: log, Timeout: cfg.RPCTimeout}
	svcboot.FatalIf(log, rpc.Serve(wiring, cfg.RPCPrefix, eng, rpc.NewAuthorizer(nc, rpc.AuthSubject(cfg.UsersAuthSubject))),
		"failed to serve deploy rpc")
	svcboot.ServeHealth(svcboot.Health{
		Log: log, NC: nc, Service: serviceName, QueueGroup: queueGroup, ListenAddr: core.ListenAddr,
	})
	log.Info("deployer ready")

	core.Await()
	awaitEngine(stopped, log)
}

func openStore(core svcboot.Core, cfg *config.Config) (*store.Store, func()) {
	js, closeJS, err := bus.OpenCoordination(core.NATSURL)
	svcboot.FatalIf(core.Log, err, "failed to connect run store")
	setupCtx, cancel := context.WithTimeout(core.Ctx, storeSetupTimeout)
	defer cancel()
	runs, err := store.New(setupCtx, js, cfg.Deploy)
	svcboot.FatalIf(core.Log, err, "failed to open run store")
	return runs, closeJS
}

func runEngine(ctx context.Context, eng *engine.Engine, log *zap.Logger) <-chan struct{} {
	stopped := make(chan struct{})
	go func() {
		defer close(stopped)
		err := eng.Run(ctx)
		if ctx.Err() == nil {
			log.Fatal("engine stopped", zap.Error(err))
		}
	}()
	return stopped
}

func awaitEngine(stopped <-chan struct{}, log *zap.Logger) {
	select {
	case <-stopped:
	case <-time.After(engineDrainTimeout):
		log.Warn("engine did not stop before the deadline, the next pod resumes the run")
	}
}

func stageDeps(cfg *config.Config, kube *rest.Config, log *zap.Logger) stage.Deps {
	hub, err := gh.New(gh.Config{
		AppID: cfg.GitHubAppID, InstallationID: cfg.GitHubInstallationID,
		PrivateKey: cfg.GitHubPrivateKey, Deploy: cfg.Deploy,
	})
	svcboot.FatalIf(log, err, "failed to build github client")
	applier, err := apply.New(kube)
	svcboot.FatalIf(log, err, "failed to build applier")
	watcher, err := watch.New(kube, cfg.Deploy, &http.Client{Timeout: httpTimeout})
	svcboot.FatalIf(log, err, "failed to build watcher")

	return stage.Deps{
		GitHub:   hub,
		Registry: registry.New(registry.Config{Repo: cfg.Deploy.ImageRepo, Username: cfg.GHCRUsername, Password: cfg.GHCRToken}),
		Applier:  applier,
		Watcher:  watcher,
		Clock:    ports.SystemClock{},
		Config:   cfg.Deploy,
		Log:      log,
	}
}
