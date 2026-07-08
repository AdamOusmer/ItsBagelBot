package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"
	"time"

	"ItsBagelBot/app/outgress/internal/channels"
	"ItsBagelBot/app/outgress/internal/conduit"
	"ItsBagelBot/app/outgress/internal/config"
	"ItsBagelBot/app/outgress/internal/tokenstore"
	"ItsBagelBot/app/outgress/internal/twitch"
	"ItsBagelBot/app/outgress/internal/worker"
	"ItsBagelBot/app/outgress/rpc"
	"ItsBagelBot/pkg/bus"
	"ItsBagelBot/pkg/env"
	"ItsBagelBot/pkg/health"
	"ItsBagelBot/pkg/logger"
	"ItsBagelBot/pkg/monitor"
	"ItsBagelBot/pkg/ratelimit"
	pkg_valkey "ItsBagelBot/pkg/valkey"

	"github.com/ThreeDotsLabs/watermill/message"
	"github.com/nats-io/nats.go"
	"github.com/newrelic/go-agent/v3/newrelic"
	valkey_go "github.com/valkey-io/valkey-go"

	"go.uber.org/zap"
)

const serviceName = "outgress"

// A failed command is retried three times at one-second intervals. The
// work-queue stream also has a five-second MaxAge, so it cannot survive a
// restart and reappear later as stale chat output.
//
// System (EventSub enroll / stream_status) jobs live on their own stream with
// a five-minute MaxAge, so their retries are slower and more numerous: a
// transient Twitch or rate-limit failure gets another shot every fifteen
// seconds for as long as the message survives.
const (
	nakDelay        = time.Second
	maxRedeliveries = 3

	systemNakDelay        = 15 * time.Second
	systemMaxRedeliveries = 6
)

// fatalIf aborts startup on err: outgress cannot run degraded without any of
// its core dependencies, so a failed step must crash the pod for Kubernetes to
// restart it.
func fatalIf(log *zap.Logger, err error, msg string) {
	if err != nil {
		log.Fatal(msg, zap.Error(err))
	}
}

// deps carries the process-wide handles main assembles once and every later
// wiring step reads from.
type deps struct {
	cfg    *config.Config
	log    *zap.Logger
	nrApp  *newrelic.Application
	nc     *nats.Conn
	valkey valkey_go.Client
	host   string
}

func main() {
	log := logger.New(env.Get("APP_ENV", "development")).Named(serviceName)
	defer func() { _ = log.Sync() }()

	nrApp, err := monitor.New(serviceName, log)
	fatalIf(log, err, "failed to start new relic")
	log = monitor.WrapLogger(log, nrApp)
	defer monitor.Shutdown(nrApp)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	cfg := config.Load()
	warnStartupFallbacks(cfg, log)

	// Reconcile both outgress streams here (not only from producer services) so
	// their retention and lifetimes are guaranteed before any lane consumer
	// attaches. Order matters: the chat stream is narrowed off the system subject
	// FIRST, so adding the system stream cannot overlap it. The chat lanes are
	// perishable work-queue (5s); the control lane keeps a longer lifetime so an
	// EventSub enroll survives a rollout gap instead of being purged.
	fatalIf(log, bus.EnsureStreams(ctx, cfg.NATSURL, []bus.StreamSpec{bus.OutgressStream, bus.OutgressSystemStream}, log),
		"failed to provision outgress streams")

	valkeyClient, err := pkg_valkey.NewClient(cfg.ValkeyAddr, cfg.ValkeyPassword)
	fatalIf(log, err, "failed to connect to valkey")
	defer valkeyClient.Close()

	registry := channels.New(valkeyClient)

	nc, err := bus.Connect(cfg.NATSRPCURL, serviceName)
	fatalIf(log, err, "failed to connect to nats")
	defer nc.Close()
	fatalIf(log, registry.StartInvalidationListener(nc, cfg.CacheInvalidatePrefix, log.Named("channels")),
		"failed to subscribe channel cache invalidation")
	defer registry.Close()

	host := podIdentity(log)
	// Label every worker transaction with this pod's region and the Kubernetes
	// node it runs on so the Twitch external-segment duration can be split per
	// node in New Relic. NODE_NAME (spec.nodeName) names the actual node;
	// hostname (the pod) is the dev fallback.
	worker.SetNodeIdentity(cfg.RateRegion, env.Get("NODE_NAME", host))

	d := &deps{cfg: cfg, log: log, nrApp: nrApp, nc: nc, valkey: valkeyClient, host: host}

	tw := d.newTwitchClient()
	defer tw.CloseIdleConnections()
	warmupTwitch(ctx, tw, log)

	limiter, closeLimiter := d.newLeaseLimiter(ctx)
	defer closeLimiter()

	premium, standard, system, closeWorkers := d.newLaneWorkers(tw, limiter, registry)
	defer closeWorkers()

	premiumSub, standardSub, systemSub, closeSubs := d.laneSubscribers()
	defer closeSubs()

	d.startChatLanes(ctx, []bus.WeightedLane{
		{Sub: premiumSub, Subject: cfg.PremiumSubject, Handle: premium.Process, Reserve: cfg.PremiumReserve},
		{Sub: standardSub, Subject: cfg.StandardSubject, Handle: standard.Process},
	})
	d.startSystemLane(ctx, systemSub, system)

	closeStreamLane := d.startStreamLane(ctx, system)
	defer closeStreamLane()

	fatalIf(log, rpc.SubscribeManage(nc, registry, tw, cfg.RPCPrefix, "outgress-rpc", nrApp, log.Named("rpc")),
		"failed to subscribe management rpc")

	health.Serve(env.Get("LISTEN_ADDR", ":8080"), nc.IsConnected)

	d.logReady(tw)

	<-ctx.Done()

	log.Info("outgress shutting down")
}

// warnStartupFallbacks surfaces the degradable startup conditions. The
// deployment supplies a stable locality for the quota-lease protocol; keep the
// config fallback usable so a missing optional tuning value cannot turn an
// otherwise healthy outgress rollout into a fleet-wide outage.
func warnStartupFallbacks(cfg *config.Config, log *zap.Logger) {
	if os.Getenv("OUTGRESS_REGION") == "" {
		log.Warn("OUTGRESS_REGION is unset; using fallback locality",
			zap.String("rate_region", cfg.RateRegion))
	}
	if err := worker.PrepareJSON(); err != nil {
		log.Warn("failed to precompile outgress JSON decoders", zap.Error(err))
	}
}

// podIdentity returns this pod's stable identity, used only for lease
// membership and targeted permits; it never assigns broadcaster ownership.
func podIdentity(log *zap.Logger) string {
	host, err := os.Hostname()
	if err != nil || host == "" {
		log.Fatal("failed to determine outgress pod identity", zap.Error(err))
	}
	return host
}

// newTwitchClient assembles the Helix client over the three token sources: the
// app token, the bot account's user token, and the per-broadcaster grants.
func (d *deps) newTwitchClient() *twitch.Client {
	appTokens := twitch.NewAppTokenSource(d.cfg.TwitchClientID, d.cfg.TwitchClientSecret)
	return twitch.NewClient(d.cfg.TwitchClientID, appTokens, d.botTokenSource(), d.broadcasterTokens())
}

// botTokenSource builds the bot account's user token source. It prefers the
// copy stored by the users service (the admin panel manages it); the env
// refresh token is only a seed or, without a bot user id, the legacy static
// configuration. nil (with a warning) disables mod status verification.
func (d *deps) botTokenSource() *twitch.Source {
	switch {
	case d.cfg.TwitchBotUserID != "":
		return d.storedTokenSource(d.cfg.TwitchBotUserID, d.cfg.TwitchBotRefreshToken)
	case d.cfg.TwitchBotRefreshToken != "":
		return twitch.NewUserTokenSource(d.cfg.TwitchClientID, d.cfg.TwitchClientSecret, d.cfg.TwitchBotRefreshToken)
	default:
		d.log.Warn("no bot user id or refresh token configured, mod status verification disabled")
		return nil
	}
}

// broadcasterTokens wires the per-broadcaster user tokens: a job with
// as="broadcaster" sends under the channel's own stored grant (saved by the
// dashboard at login) rather than the bot. Each Source loads/persists that
// channel's refresh token through the same users-service token RPC, keyed by
// broadcaster id.
func (d *deps) broadcasterTokens() *twitch.BroadcasterTokens {
	return twitch.NewBroadcasterTokens(func(broadcasterID string) *twitch.Source {
		return d.storedTokenSource(broadcasterID, "")
	})
}

// storedTokenSource builds a user token source backed by the users-service
// token store for one account, seeded with an optional env refresh token.
func (d *deps) storedTokenSource(accountID, seedRefresh string) *twitch.Source {
	store := tokenstore.New(d.nc, d.cfg.TokensSubjectPrefix, accountID)
	log := d.log
	return twitch.NewStoredUserTokenSource(
		d.cfg.TwitchClientID, d.cfg.TwitchClientSecret, seedRefresh,
		func(ctx context.Context) string {
			refresh, err := store.Load(ctx)
			if err != nil {
				log.Debug("stored token unavailable", zap.String("account_id", accountID), zap.Error(err))
				return ""
			}
			return refresh
		},
		func(ctx context.Context, access, refresh string) {
			if err := store.Save(ctx, access, refresh); err != nil {
				log.Warn("token persist failed", zap.String("account_id", accountID), zap.Error(err))
			}
		},
	)
}

// warmupTwitch pays the cold-start cost (token minting, DNS/TLS and the first
// HTTP/2 handshake) before consumers and readiness come online, instead of on
// the first real chat message handled by each new pod. A transient Twitch
// outage must not crash-loop the service, so the bounded warmup degrades to a
// warning.
func warmupTwitch(ctx context.Context, tw *twitch.Client, log *zap.Logger) {
	warmupStarted := time.Now()
	warmupCtx, warmupCancel := context.WithTimeout(ctx, 8*time.Second)
	err := tw.Warmup(warmupCtx)
	warmupCancel()
	if err != nil {
		log.Warn("twitch warmup failed; continuing with lazy retry",
			zap.Duration("duration", time.Since(warmupStarted)), zap.Error(err))
		return
	}
	log.Info("twitch client warmed", zap.Duration("duration", time.Since(warmupStarted)))
}

// newLeaseLimiter assembles the lease-based rate limiter: the local bucket
// store, the permit RPC service, the lease manager, and its coordinator. The
// returned cleanup releases them in reverse order.
func (d *deps) newLeaseLimiter(ctx context.Context) (ratelimit.Manager, func()) {
	// Sized for the working set of lease buckets (active chat channels plus the
	// fixed Helix buckets); DeleteExpired prunes idle channels every epoch and
	// the store grows past the presize if a burst ever needs it.
	buckets := ratelimit.NewBucketStore(2048)

	permitSvc, err := ratelimit.NewPermitService(d.nc, d.cfg.RateRegion, d.host, buckets)
	fatalIf(d.log, err, "failed to initialize permit service")

	limiter := ratelimit.NewLeaseManager(ratelimit.New(d.valkey), buckets, permitSvc,
		ratelimit.WithLeaseIdentity(d.cfg.RateRegion, d.host))
	permitSvc.SetGrantor(limiter)

	coordinator := ratelimit.NewLeaseCoordinator(d.nc, d.valkey, limiter, d.cfg.RateRegion, d.host,
		ratelimit.CoordinatorConfig{
			Epoch: d.cfg.LeaseEpoch, Guard: d.cfg.LeaseGuard, MinMembers: d.cfg.LeaseMinMembers,
			Replicas: d.cfg.LeaseReplicas, ReplicaTimeout: d.cfg.LeaseReplicaTimeout,
		}, d.log.Named("leases"))
	fatalIf(d.log, coordinator.Start(ctx), "failed to initialize lease coordinator")

	return limiter, func() {
		coordinator.Close()
		permitSvc.Close()
	}
}

// newLaneWorkers builds the three lane workers over the shared collaborators,
// plus the mod verifier and live writer they hang off. The system lane carries
// the dashboard's EventSub create/delete jobs; it pays only the reserved
// system Helix partition, so onboarding bursts never compete with chat/api
// traffic for the general budget. It also resolves live re-checks
// (stream_status jobs) and writes the result back into the live projection for
// the worker fleet.
func (d *deps) newLaneWorkers(tw *twitch.Client, limiter ratelimit.Manager, registry *channels.Registry) (premium, standard, system *worker.Worker, cleanup func()) {
	base := worker.Config{
		Limiter:  limiter,
		Registry: registry,
		Twitch:   tw,
		BotID:    d.cfg.TwitchBotUserID,
		Owner:    d.host,
		Conduit:  conduit.New(d.nc, d.cfg.ConduitSubject, d.cfg.TwitchConduitID, 60*time.Second, d.log.Named("conduit")),
	}
	build := func(name string, lane worker.Lane) *worker.Worker {
		cfg := base
		cfg.Log = d.log.Named(name)
		cfg.Lane = lane
		return worker.New(cfg)
	}
	premium = build("premium", worker.LanePremium)
	standard = build("standard", worker.LaneStandard)
	system = build("system", worker.LaneSystem)

	modVerifier := worker.NewModVerifier(registry, tw, d.cfg.TwitchBotUserID, d.host, d.log.Named("mod-status"))
	premium.SetModVerifier(modVerifier)
	standard.SetModVerifier(modVerifier)
	system.SetModVerifier(modVerifier)
	system.SetLiveWriter(worker.NewLiveWriter(d.valkey, d.nc, d.cfg.CacheInvalidatePrefix, d.cfg.LiveTTL, d.log.Named("live")))

	return premium, standard, system, modVerifier.Close
}

// laneSubscribers connects the three lane subscribers; paced redelivery keeps
// rate-limit nacks from spinning.
func (d *deps) laneSubscribers() (premiumSub, standardSub, systemSub message.Subscriber, closeAll func()) {
	var err error
	premiumSub, err = bus.NewLaneSubscriber(bus.LaneConfig{
		URL: d.cfg.NATSURL, Stream: bus.OutgressStream.Name, Subject: d.cfg.PremiumSubject,
		Group: "outgress-premium", NakDelay: nakDelay, MaxRedeliveries: maxRedeliveries,
	}, d.log)
	fatalIf(d.log, err, "failed to connect premium subscriber")

	standardSub, err = bus.NewLaneSubscriber(bus.LaneConfig{
		URL: d.cfg.NATSURL, Stream: bus.OutgressStream.Name, Subject: d.cfg.StandardSubject,
		Group: "outgress-standard", NakDelay: nakDelay, MaxRedeliveries: maxRedeliveries,
	}, d.log)
	fatalIf(d.log, err, "failed to connect standard subscriber")

	systemSub, err = bus.NewLaneSubscriber(bus.LaneConfig{
		URL: d.cfg.NATSURL, Stream: bus.OutgressSystemStream.Name, Subject: d.cfg.SystemSubject,
		Group: "outgress-system", NakDelay: systemNakDelay, MaxRedeliveries: systemMaxRedeliveries,
	}, d.log)
	fatalIf(d.log, err, "failed to connect system subscriber")

	return premiumSub, standardSub, systemSub, func() {
		_ = systemSub.Close()
		_ = standardSub.Close()
		_ = premiumSub.Close()
	}
}

// startChatLanes runs premium and standard on one central weighted consumer: a
// single routine budget partitioned by weight so premium drains ahead without
// starving standard.
func (d *deps) startChatLanes(ctx context.Context, lanes []bus.WeightedLane) {
	fatalIf(d.log, bus.ConsumeWeighted(ctx, d.nrApp, lanes, bus.ScalePolicy{
		MinRoutines:    d.cfg.MinRoutines,
		MaxRoutines:    d.cfg.MaxRoutines,
		MaxConsumers:   d.cfg.MaxConsumers,
		ScaleUpAfter:   d.cfg.ScaleUpAfter,
		ScaleDownAfter: d.cfg.ScaleDownAfter,
	}, d.log), "failed to consume premium/standard lanes")
}

// startSystemLane keeps the system lane on its own independent consumer, off
// the weighted budget, so onboarding bursts never compete for the chat/api
// routines. It runs a fixed pool (min == max, single consumer), no autoscaling.
func (d *deps) startSystemLane(ctx context.Context, sub message.Subscriber, system *worker.Worker) {
	fatalIf(d.log, bus.ConsumeWeighted(ctx, d.nrApp, []bus.WeightedLane{
		{Sub: sub, Subject: d.cfg.SystemSubject, Handle: system.Process},
	}, bus.ScalePolicy{
		MinRoutines:  d.cfg.SystemWorkers,
		MaxRoutines:  d.cfg.SystemWorkers,
		MaxConsumers: 1,
	}, d.log), "failed to consume system lane")
}

// startStreamLane binds a durable consumer for the real Twitch stream.online /
// stream.offline events on the ingress stream lane (TWITCH_INGRESS,
// provisioned by ingress/projector) under outgress's OWN service group, so the
// system worker re-verifies the bot's mod status on every go-live. This
// restores the re-verify that used to ride the cold-live escalation: once the
// projector writes the live key directly from these events, the worker's live
// query is no longer cold, so stream_status (and its mod-status re-check) no
// longer fires. The projector binds its own group on the same subject and
// still gets every event once. Best-effort and idempotent: HandleStreamEvent
// only re-verifies, never writes live state (that is the projector's job).
func (d *deps) startStreamLane(ctx context.Context, system *worker.Worker) func() {
	streamSub, err := bus.NewSubscriber(d.cfg.NATSURL, serviceName, d.log)
	fatalIf(d.log, err, "failed to connect stream-lane subscriber")

	fatalIf(d.log, bus.Consume(ctx, d.nrApp, streamSub, d.cfg.StreamLaneSubject, system.HandleStreamEvent, d.log),
		"failed to consume stream lane")

	return func() { _ = streamSub.Close() }
}

func (d *deps) logReady(tw *twitch.Client) {
	d.log.Info("outgress ready",
		zap.String("premium_subject", d.cfg.PremiumSubject),
		zap.String("standard_subject", d.cfg.StandardSubject),
		zap.String("rpc_prefix", d.cfg.RPCPrefix),
		zap.String("stream_lane_subject", d.cfg.StreamLaneSubject),
		zap.Bool("mod_verification", tw.HasUserToken()),
		zap.Int("min_routines", d.cfg.MinRoutines),
		zap.Int("max_routines", d.cfg.MaxRoutines),
		zap.Int("max_consumers", d.cfg.MaxConsumers),
		zap.Int("premium_reserve_percent", d.cfg.PremiumReserve),
		zap.Int("system_workers", d.cfg.SystemWorkers))
}
