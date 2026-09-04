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
// durable consumer per subject: same pattern app/twitch/outgress's stream/authz
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
	// linkguard.New panics on a nil client (deliberately -- see its own
	// doc), which is why it is built here, right next to the Fatal above
	// that already guarantees valkeyClient is live, rather than deferred
	// to modules.All where a nil would be easy to pass by accident.
	guard := linkguard.New(valkeyClient)

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
	// ownInvite shares valkeyClient with guard and store above -- it is
	// just another Valkey-backed cache, not a second connection -- and
	// shares rpc (rpcclient.Client) with Channels/Purge below, since
	// ResolveInvite is one more method on the same outgress RPC client
	// those already use.
	ownInvite := modules.NewOwnInviteChecker(rpc, invitecache.New(valkeyClient))

	identity := &modules.Identity{
		Resolve: resolver.ByBroadcaster,
		Status:  statusReader(projStore),
		Applied: identitystore.New(valkeyClient),
		Publish: publish,
		Log:     log,
	}
	reg := registry.New(modules.All(modules.Deps{
		Store: store, Channels: rpc, Purge: rpc, Guard: guard, OwnInvite: ownInvite,
		Identity: identity, Log: log,
	})...)
	d := &dispatch.Dispatcher{Registry: reg, Resolver: resolver, Store: store, Publish: publish, Log: log}

	closeIngress := startIngressConsumers(ctx, cfg, nrApp, log, d.Handle)
	defer closeIngress()

	closeTwitch := startTwitchConsumers(twitchDeps{
		Ctx: ctx, Cfg: cfg, NRApp: nrApp, Log: log,
		Resolver: resolver, ProjStore: projStore, Publish: publish, RPC: rpc, NC: nc, Identity: identity,
	})
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

// twitchDeps is every input startTwitchConsumers needs, including the
// (ctx, cfg, nrApp, log) preamble it shares with startIngressConsumers.
// First pass collapsed only the module wiring into a struct and left the
// preamble as four more positional parameters, which still tripped
// CodeScene's Excess Number of Function Arguments (5 params, over its
// 4-parameter limit). Folding the preamble in too gets the function down to
// the one parameter this struct provides.
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

// startTwitchConsumers binds Live and Clip to their Twitch inputs, on one
// shared subscriber -- the same "one Subscriber spans both inputs" pattern
// app/projector and the old dingress egress role already use.
func startTwitchConsumers(deps twitchDeps) func() {
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
	// Account facts, for the bot's per-guild appearance. Subscribed here
	// rather than left to GUILD_CREATE alone so an upgrade is visible at once
	// instead of at the next gateway reconnect, which may be hours away.
	if err := bus.Consume(deps.Ctx, deps.NRApp, sub, deps.Cfg.UserChangedSubject, deps.Identity.HandleUserChanged, deps.Log); err != nil {
		deps.Log.Fatal("failed to consume user-changed lane", zap.Error(err))
	}
	return func() { _ = sub.Close() }
}

// statusReader adapts the projection store to modules.StatusReader. An empty
// status means the user is not projected at all, which the identity module
// treats as "leave the appearance alone" rather than as "free" -- see its
// onGuild comment for why guessing free is the dangerous direction.
func statusReader(p *projection.Store) modules.StatusReader {
	return func(ctx context.Context, broadcasterID uint64) (string, bool) {
		status, _, _, _, err := p.GetUser(ctx, broadcasterID)
		if err != nil || status == "" {
			return "", false
		}
		return status, true
	}
}
