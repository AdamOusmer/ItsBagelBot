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

// The two sibling services engine answers for. Each string is that service's
// own package-main serviceName const (app/discord/ingress/main.go and
// app/discord/outgress/main.go); they are restated here because a const in
// package main is not importable across binaries, and they are the RPC health
// tokens, so they have to stay byte-identical to those declarations.
const (
	ingressService  = "discord-ingress"
	outgressService = "discord-outgress"
)

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
	core, done := svcboot.NewCore(serviceName)
	defer done()
	log, ctx, nrApp := core.Log, core.Ctx, core.NR

	cfg := config.Load()

	// Engine is DISCORD_INGRESS's consumer, so it reconciles the stream
	// (see pkg/bus.DiscordIngressStream's doc); it never provisions
	// DISCORD_OUTGRESS, the stream it only ever publishes onto -- that is
	// app/discord/outgress's job, as the consumer on that side.
	svcboot.FatalIf(log, bus.EnsureStreams(ctx, cfg.NATSURL, []bus.StreamSpec{bus.DiscordIngressStream}, log),
		"failed to provision the DISCORD_INGRESS stream")

	valkeyClient := svcboot.MustValkey(core)
	defer valkeyClient.Close()
	projStore := projection.NewStore(valkeyClient)
	// linkguard.New panics on a nil client (deliberately -- see its own
	// doc), which is why it is built here, right next to the Fatal above
	// that already guarantees valkeyClient is live, rather than deferred
	// to modules.All where a nil would be easy to pass by accident.
	guard := linkguard.New(valkeyClient)

	nc := svcboot.MustRPCConn(core, cfg.NATSRPCURL)
	defer nc.Close()

	// The store is discord-data-backed, not pure Valkey: bindings, per-guild
	// settings, tickets and XP live in MySQL now, and Valkey keeps only the
	// caches and the ephemeral voice/desk keyspaces. Built here rather than
	// beside the Valkey client above because it needs the NATS connection.
	store := discordstore.NewRPC(nc, cfg.DiscordDataRPCPrefix, valkeyClient, log)

	pub, err := bus.NewPublisher(cfg.NATSURL, log)
	svcboot.FatalIf(log, err, "failed to connect publisher")
	defer func() { _ = pub.Close() }()
	publish := confirmedPublisher(pub)

	rpc := rpcclient.New(nc, cfg.DiscordOutgressRPCPrefix)
	resolver := resolve.Resolver{Store: store, Modules: projStore, Tier: resolve.Status(statusReader(projStore)),
		Warned: resolve.NewConfigWarnings(), Log: log}
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

// confirmedPublisher emits one Command and waits for the broker's own
// verdict on it.
//
// It is deliberately not bus.PublishJSON. That path admits the payload to
// the background batch publisher and returns as soon as a worker takes it,
// so the only errors it can ever produce are "bus: publisher is closed" and
// the caller's own context error (pkg/bus/batch_publisher.go, admit and
// admitLocked). The cohort's real outcome -- a refused send, a rejected
// PubAck, a PubAck timeout -- is recorded on the connection for a later
// Flush instead (complete, takeWindowErrLocked), and nothing in this
// process calls Flush. dispatch classifies publish failures to decide
// whether a republish is safe, and classifying an error the broker never
// produced classifies nothing.
//
// bus.PublishConfirmed goes through PublishOwnedWithID, which parks on the
// cohort's verdict channel (awaitPublishConfirmation) and hands back the
// error the batch worker resolved the cohort with. The ID is required by
// that call and is not a deduplication key -- see Publisher's own doc for
// why fleet publishing has none -- so a fresh NUID per attempt is honest
// about what it is.
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

// verticalChecks is the whole Discord vertical's health surface, not just this
// process's. health.itsbagelbot.com/discord terminates in engine, so this Set
// has to answer for ingress and outgress too: neither of them is routed from
// outside, and a vertical that reported only its middle process would call the
// whole thing healthy with the gateway session dead.
//
// The two HealthProbe checks are deliberately not wrapped in health.Degrades.
// HealthProbe already carries the downstream's own verdict -- a down sibling
// fails this check, a degraded one degrades it -- so degrading it again here
// would flatten a real outage on ingress or outgress into an impairment and
// leave this pod in rotation answering for a vertical that cannot act.
func verticalChecks(nc *nats.Conn, ingressSub, twitchSub bus.Subscriber) []health.Check {
	return []health.Check{
		// One check per durable, named for the lane rather than rolled into
		// one boolean: either group can be bound and failing to fetch while
		// the connection stays green, and that is the silent failure the
		// whole vertical's dispatch stops on.
		bus.LaneCheck("ingress", ingressSub),
		bus.LaneCheck("twitch", twitchSub),
		bus.HealthProbe(nc, ingressService),
		bus.HealthProbe(nc, outgressService),
	}
}

// startIngressConsumers binds one durable consumer per DiscordIngressStream
// subject, all sharing the one dispatcher.
//
// The subscriber comes back alongside the close func because verticalChecks needs
// it: the close func alone says nothing about whether these consumers are
// still fetching.
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
// app/projector and the old dingress egress role already use. It is returned
// with the close func for the same reason startIngressConsumers returns its
// own: verticalChecks checks it.
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
	// Account facts, for the bot's per-guild appearance. Subscribed here
	// rather than left to GUILD_CREATE alone so an upgrade is visible at once
	// instead of at the next gateway reconnect, which may be hours away.
	if err := bus.Consume(deps.Ctx, deps.NRApp, sub, deps.Cfg.UserChangedSubject, deps.Identity.HandleUserChanged, deps.Log); err != nil {
		deps.Log.Fatal("failed to consume user-changed lane", zap.Error(err))
	}
	return sub, func() { _ = sub.Close() }
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
