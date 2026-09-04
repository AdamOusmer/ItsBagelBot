// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// Command engine is the Discord half of the ingress/engine/outgress split
// that decides: it consumes every discord.ingress.event.* subject plus the
// two Twitch subjects the go-live/clip modules need, and emits
// discord.outgress.{mod,default} Commands (and, for the handful of
// operations a Command cannot express, calls app/discord/outgress's
// internal RPC -- see internal/domain/rpc/discordoutgress). It never calls
// Discord itself; see app/discord/outgress for every REST call.
package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"ItsBagelBot/app/discord/engine/internal/config"
	"ItsBagelBot/app/discord/engine/internal/dispatch"
	"ItsBagelBot/app/discord/engine/internal/registry"
	"ItsBagelBot/app/discord/engine/internal/resolve"
	"ItsBagelBot/app/discord/engine/internal/rpcclient"
	"ItsBagelBot/app/discord/engine/internal/streaminfo"
	"ItsBagelBot/app/discord/engine/modules"
	"ItsBagelBot/internal/discordstore"
	ddiscord "ItsBagelBot/internal/domain/discord"
	"ItsBagelBot/internal/projection"
	"ItsBagelBot/pkg/bus"
	"ItsBagelBot/pkg/env"
	"ItsBagelBot/pkg/health"
	"ItsBagelBot/pkg/logger"
	"ItsBagelBot/pkg/monitor"
	pkg_valkey "ItsBagelBot/pkg/valkey"

	"github.com/nats-io/nats.go"
	"github.com/newrelic/go-agent/v3/newrelic"
	"go.uber.org/zap"
)

const serviceName = "discord-engine"

// ingressSubjects is DiscordIngressStream's own subject list, bound one
// durable consumer per subject: same pattern app/outgress's stream/authz
// lanes and the old dingress egress role already use for a fixed, known
// subject set.
var ingressSubjects = []string{
	ddiscord.SubjectEventMessage,
	ddiscord.SubjectEventMember,
	ddiscord.SubjectEventVoice,
	ddiscord.SubjectEventInteraction,
	ddiscord.SubjectEventAudit,
	ddiscord.SubjectEventGuild,
}

func main() {
	log := logger.New(env.Get("APP_ENV", "development")).Named(serviceName)
	defer func() { _ = log.Sync() }()

	nrApp, err := monitor.New(serviceName, log)
	if err != nil {
		log.Fatal("failed to start new relic", zap.Error(err))
	}
	log = monitor.WrapLogger(log, nrApp)
	defer monitor.Shutdown(nrApp)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	cfg := config.Load()

	// Engine is DISCORD_INGRESS's consumer, so it reconciles the stream
	// (see pkg/bus.DiscordIngressStream's doc); it never provisions
	// DISCORD_OUTGRESS, the stream it only ever publishes onto -- that is
	// app/discord/outgress's job, as the consumer on that side.
	if err := bus.EnsureStreams(ctx, cfg.NATSURL, []bus.StreamSpec{bus.DiscordIngressStream}, log); err != nil {
		log.Fatal("failed to provision the DISCORD_INGRESS stream", zap.Error(err))
	}

	valkeyClient, err := pkg_valkey.NewClient(cfg.ValkeyAddr, cfg.ValkeyPassword)
	if err != nil {
		log.Fatal("failed to connect to valkey", zap.Error(err))
	}
	defer valkeyClient.Close()
	store := discordstore.New(valkeyClient)
	projStore := projection.NewStore(valkeyClient)

	nc, err := bus.Connect(cfg.NATSRPCURL, serviceName)
	if err != nil {
		log.Fatal("failed to connect to nats", zap.Error(err))
	}
	defer nc.Close()

	pub, err := bus.NewPublisher(cfg.NATSURL, log)
	if err != nil {
		log.Fatal("failed to connect publisher", zap.Error(err))
	}
	defer func() { _ = pub.Close() }()
	publish := func(ctx context.Context, c ddiscord.Command) error {
		return bus.PublishJSON(ctx, pub, ddiscord.Lane(c.Type), c)
	}

	rpc := rpcclient.New(nc, cfg.DiscordOutgressRPCPrefix)
	resolver := resolve.Resolver{Store: store, Modules: projStore, Log: log}

	reg := registry.New(modules.All(modules.Deps{Store: store, Channels: rpc, Purge: rpc, Log: log})...)
	d := &dispatch.Dispatcher{Registry: reg, Resolver: resolver, Store: store, Publish: publish, Log: log}

	closeIngress := startIngressConsumers(ctx, cfg, nrApp, log, d.Handle)
	defer closeIngress()

	closeTwitch := startTwitchConsumers(ctx, cfg, nrApp, log, resolver, projStore, publish, rpc, nc)
	defer closeTwitch()

	health.Serve(cfg.ListenAddr, serviceName, health.NATS("nats", nc))
	log.Info("discord engine ready", zap.Strings("ingress_subjects", ingressSubjects))

	<-ctx.Done()
	log.Info("discord engine shutting down")
}

// startIngressConsumers binds one durable consumer per DiscordIngressStream
// subject, all sharing the one dispatcher.
func startIngressConsumers(ctx context.Context, cfg config.Config, nrApp *newrelic.Application, log *zap.Logger, handle func(*bus.Message) error) func() {
	sub, err := bus.NewSubscriber(cfg.NATSURL, serviceName, log)
	if err != nil {
		log.Fatal("failed to connect discord ingress subscriber", zap.Error(err))
	}
	for _, subject := range ingressSubjects {
		if err := bus.Consume(ctx, nrApp, sub, subject, handle, log); err != nil {
			log.Fatal("failed to consume discord ingress subject", zap.String("subject", subject), zap.Error(err))
		}
	}
	return func() { _ = sub.Close() }
}

// startTwitchConsumers binds Live and Clip to their Twitch inputs, on one
// shared subscriber -- the same "one Subscriber spans both inputs" pattern
// app/projector and the old dingress egress role already use.
func startTwitchConsumers(
	ctx context.Context, cfg config.Config, nrApp *newrelic.Application, log *zap.Logger,
	resolver resolve.Resolver, projStore *projection.Store, publish modules.Publish, rpc *rpcclient.Client, nc *nats.Conn,
) func() {
	live := &modules.Live{
		Resolve:    resolver.ByBroadcaster,
		StreamInfo: projStore,
		Fallback:   streaminfo.New(nc, cfg.TwitchOutgressRPCPrefix),
		RPC:        rpc,
		Publish:    publish,
		Log:        log,
	}
	clip := &modules.Clip{Resolve: resolver.ByBroadcaster, Publish: publish, Log: log}

	sub, err := bus.NewSubscriber(cfg.NATSURL, serviceName, log)
	if err != nil {
		log.Fatal("failed to connect twitch-lane subscriber", zap.Error(err))
	}
	if err := bus.Consume(ctx, nrApp, sub, cfg.StreamLaneSubject, live.HandleStreamEvent, log); err != nil {
		log.Fatal("failed to consume stream lane", zap.Error(err))
	}
	if err := bus.Consume(ctx, nrApp, sub, cfg.ClipCreatedSubject, clip.HandleClipCreated, log); err != nil {
		log.Fatal("failed to consume clip-created lane", zap.Error(err))
	}
	return func() { _ = sub.Close() }
}
