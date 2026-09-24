// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package main

import (
	"context"

	"ItsBagelBot/app/discord/engine/internal/config"
	"ItsBagelBot/app/discord/engine/internal/dispatch"
	"ItsBagelBot/app/discord/engine/internal/identitystore"
	"ItsBagelBot/app/discord/engine/internal/invitecache"
	"ItsBagelBot/app/discord/engine/internal/registry"
	"ItsBagelBot/app/discord/engine/internal/resolve"
	"ItsBagelBot/app/discord/engine/internal/rpcclient"
	"ItsBagelBot/app/discord/engine/internal/streaminfo"
	"ItsBagelBot/app/discord/engine/modules"
	"ItsBagelBot/internal/discordstore"
	ddiscord "ItsBagelBot/internal/domain/discord"
	"ItsBagelBot/internal/domain/discord/linkguard"
	"ItsBagelBot/internal/projection"
	"ItsBagelBot/pkg/bus"
	"ItsBagelBot/pkg/codec"
	"ItsBagelBot/pkg/health"
	"ItsBagelBot/pkg/svcboot"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nuid"
	"github.com/newrelic/go-agent/v3/newrelic"
	"go.uber.org/zap"
)

const serviceName = "discord-engine"

// Must match the serviceName consts in app/discord/ingress/main.go and app/discord/outgress/main.go.
const (
	ingressService  = "discord-ingress"
	outgressService = "discord-outgress"
)

var ingressSubjects = []string{
	ddiscord.SubjectEventMessage,
	ddiscord.SubjectEventMember,
	ddiscord.SubjectEventVoice,
	ddiscord.SubjectEventInteraction,
	ddiscord.SubjectEventAudit,
	ddiscord.SubjectEventGuild,
}

func main() {
	core, done := svcboot.NewCore(serviceName)
	defer done()
	log, ctx, nrApp := core.Log, core.Ctx, core.NR

	cfg := config.Load()

	svcboot.FatalIf(log, bus.EnsureStreams(ctx, cfg.NATSURL, []bus.StreamSpec{bus.DiscordIngressStream}, log),
		"failed to provision the DISCORD_INGRESS stream")

	valkeyClient := svcboot.MustValkey(core)
	defer valkeyClient.Close()
	projStore := projection.NewStore(valkeyClient)
	guard := linkguard.New(valkeyClient)

	nc := svcboot.MustRPCConn(core, cfg.NATSRPCURL)
	defer nc.Close()

	store := discordstore.NewRPC(nc, cfg.DiscordDataRPCPrefix, valkeyClient, log)

	pub, err := bus.NewPublisher(cfg.NATSURL, log)
	svcboot.FatalIf(log, err, "failed to connect publisher")
	defer func() { _ = pub.Close() }()
	publish := confirmedPublisher(pub)

	rpc := rpcclient.New(nc, cfg.DiscordOutgressRPCPrefix)
	resolver := resolve.Resolver{Store: store, Modules: projStore, Tier: resolve.Status(statusReader(projStore)),
		Warned: resolve.NewConfigWarnings(), Log: log}
	ownInvite := modules.NewOwnInviteChecker(rpc, invitecache.New(valkeyClient))

	identity := &modules.Identity{
		Resolve: resolver.ByBroadcaster,
		Status:  statusReader(projStore),
		Applied: identitystore.New(valkeyClient),
		Publish: publish,
		Log:     log,
	}
	reg := registry.New(modules.All(modules.Deps{
		Store: store, Channels: rpc, Tickets: rpc, Purge: rpc, Guard: guard, OwnInvite: ownInvite,
		Identity: identity, Log: log,
	})...)
	d := &dispatch.Dispatcher{Registry: reg, Resolver: resolver, Store: store, Publish: publish, Log: log}

	ingressSub, closeIngress := startIngressConsumers(ctx, cfg, nrApp, log, d.Handle)
	defer closeIngress()

	twitchSub, closeTwitch := startTwitchConsumers(twitchDeps{
		Ctx: ctx, Cfg: cfg, NRApp: nrApp, Log: log,
		Resolver: resolver, ProjStore: projStore, Publish: publish, RPC: rpc, NC: nc, Identity: identity,
	})
	defer closeTwitch()

	svcboot.ServeHealth(svcboot.Health{
		Log: log, NC: nc, Service: serviceName, QueueGroup: serviceName + "-rpc", ListenAddr: cfg.ListenAddr,
	}, verticalChecks(nc, ingressSub, twitchSub)...)
	log.Info("discord engine ready", zap.Strings("ingress_subjects", ingressSubjects))

	core.Await()
}

// Must stay confirmed: bus.PublishJSON hides the errors dispatch uses to decide a republish is safe.
func confirmedPublisher(pub bus.Publisher) modules.Publish {
	return func(ctx context.Context, c ddiscord.Command) error {
		body, err := codec.Marshal(c)
		if err != nil {
			return err
		}
		return bus.PublishConfirmed(ctx, pub, bus.Publication{
			Subject: ddiscord.Lane(c.Type),
			ID:      nuid.Next(),
			Payload: body,
		})
	}
}

// Do not wrap the probes in health.Degrades: a sibling outage must fail this vertical's health.
func verticalChecks(nc *nats.Conn, ingressSub, twitchSub bus.Subscriber) []health.Check {
	return []health.Check{
		bus.LaneCheck("ingress", ingressSub),
		bus.LaneCheck("twitch", twitchSub),
		bus.HealthProbe(nc, ingressService),
		bus.HealthProbe(nc, outgressService),
	}
}

func startIngressConsumers(ctx context.Context, cfg config.Config, nrApp *newrelic.Application, log *zap.Logger, handle func(*bus.Message) error) (bus.Subscriber, func()) {
	sub, err := bus.NewSubscriber(cfg.NATSURL, serviceName, log)
	if err != nil {
		log.Fatal("failed to connect discord ingress subscriber", zap.Error(err))
	}
	for _, subject := range ingressSubjects {
		if err := bus.Consume(ctx, nrApp, sub, subject, handle, log); err != nil {
			log.Fatal("failed to consume discord ingress subject", zap.String("subject", subject), zap.Error(err))
		}
	}
	return sub, func() { _ = sub.Close() }
}

type twitchDeps struct {
	Ctx       context.Context
	Cfg       config.Config
	NRApp     *newrelic.Application
	Log       *zap.Logger
	Resolver  resolve.Resolver
	ProjStore *projection.Store
	Publish   modules.Publish
	RPC       *rpcclient.Client
	NC        *nats.Conn
	Identity  *modules.Identity
}

func startTwitchConsumers(deps twitchDeps) (bus.Subscriber, func()) {
	live := &modules.Live{
		Resolve:    deps.Resolver.ByBroadcaster,
		StreamInfo: deps.ProjStore,
		Fallback:   streaminfo.New(deps.NC, deps.Cfg.TwitchOutgressRPCPrefix),
		RPC:        deps.RPC,
		Log:        deps.Log,
	}
	clip := &modules.Clip{Resolve: deps.Resolver.ByBroadcaster, Publish: deps.Publish, Log: deps.Log}

	sub, err := bus.NewSubscriber(deps.Cfg.NATSURL, serviceName, deps.Log)
	if err != nil {
		deps.Log.Fatal("failed to connect twitch-lane subscriber", zap.Error(err))
	}
	if err := bus.Consume(deps.Ctx, deps.NRApp, sub, deps.Cfg.StreamLaneSubject, live.HandleStreamEvent, deps.Log); err != nil {
		deps.Log.Fatal("failed to consume stream lane", zap.Error(err))
	}
	if err := bus.Consume(deps.Ctx, deps.NRApp, sub, deps.Cfg.ClipCreatedSubject, clip.HandleClipCreated, deps.Log); err != nil {
		deps.Log.Fatal("failed to consume clip-created lane", zap.Error(err))
	}
	if err := bus.Consume(deps.Ctx, deps.NRApp, sub, deps.Cfg.UserChangedSubject, deps.Identity.HandleUserChanged, deps.Log); err != nil {
		deps.Log.Fatal("failed to consume user-changed lane", zap.Error(err))
	}
	return sub, func() { _ = sub.Close() }
}

func statusReader(p *projection.Store) modules.StatusReader {
	return func(ctx context.Context, broadcasterID uint64) (string, bool) {
		status, _, _, _, _, err := p.GetUser(ctx, broadcasterID)
		if err != nil || status == "" {
			return "", false
		}
		return status, true
	}
}
