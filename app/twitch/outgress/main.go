// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"
	"time"

	"ItsBagelBot/app/twitch/outgress/internal/channels"
	"ItsBagelBot/app/twitch/outgress/internal/conduit"
	"ItsBagelBot/app/twitch/outgress/internal/config"
	"ItsBagelBot/app/twitch/outgress/internal/tokenstore"
	"ItsBagelBot/app/twitch/outgress/internal/twitch"
	"ItsBagelBot/app/twitch/outgress/internal/worker"
	"ItsBagelBot/app/twitch/outgress/rpc"
	"ItsBagelBot/internal/activity"
	"ItsBagelBot/internal/domain/i18n"
	"ItsBagelBot/internal/projection"
	"ItsBagelBot/pkg/bus"
	"ItsBagelBot/pkg/env"
	"ItsBagelBot/pkg/health"
	"ItsBagelBot/pkg/logger"
	"ItsBagelBot/pkg/monitor"
	"ItsBagelBot/pkg/ratelimit"
	pkg_valkey "ItsBagelBot/pkg/valkey"

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
	warnLocaleGaps(log)

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

	// Real Overview activity sink: the modactions.go/redemption.go Emit call
	// sites are already wired (see internal/activity's decision record) and
	// need only this one SetSink to start landing rows.
	activity.SetSink(activity.NewStore(valkeyClient))

	registry := channels.New(valkeyClient)

	nc, err := bus.Connect(cfg.NATSRPCURL, serviceName)
	fatalIf(log, err, "failed to connect to nats")
	defer nc.Close()
	fatalIf(log, registry.StartInvalidationListener(nc, cfg.CacheInvalidatePrefix, log.Named("channels")),
		"failed to subscribe channel cache invalidation")
	defer registry.Close()

	// pub is the pooled async publisher for derived-fact events outgress
	// fires after a lane handler's own work is done -- currently only the
	// clip-created fact (see worker/clip.go's publishClipCreated). It is a
	// separate connection pool from nc above (request-reply/RPC) because
	// bus.Publisher batches and pools independently of core-NATS requests.
	pub, err := bus.NewPublisher(cfg.NATSURL, log)
	fatalIf(log, err, "failed to connect publisher")
	defer func() { _ = pub.Close() }()

	host := podIdentity(log)
	// Label every worker transaction with this pod's region and the Kubernetes
	// node it runs on so the Twitch external-segment duration can be split per
	// node in New Relic. NODE_NAME (spec.nodeName) names the actual node;
	// hostname (the pod) is the dev fallback.
	worker.SetNodeIdentity(cfg.RateRegion, env.Get("NODE_NAME", host))

	d := &deps{cfg: cfg, log: log, nrApp: nrApp, nc: nc, valkey: valkeyClient, host: host}

	tw := d.newTwitchClient(ctx)
	defer tw.CloseIdleConnections()
	warmupTwitch(ctx, tw, log)

	limiter, closeLimiter := d.newLeaseLimiter(ctx)
	defer closeLimiter()

	premium, standard, system, closeWorkers := d.newLaneWorkers(tw, limiter, registry)
	defer closeWorkers()

	closeTokenWarm := d.startTokenWarmListener(system)
	defer closeTokenWarm()

	premiumSub, standardSub, systemSub, closeSubs := d.laneSubscribers()
	defer closeSubs()

	// Streamer-facing messaging attaches before ANY consumer that can reach it.
	// The system lane needs it for the go-live beacon and the authz consumers;
	// the chat lanes need it because a broadcaster-identity call failing there
	// is what discovers a dead grant in the first place, and that raises the
	// dashboard bell immediately rather than waiting for the next go-live.
	// The notifier holds no per-lane state, so one instance serves all three.
	reauth := worker.NewReauthNotifier(nc, worker.ReauthConfig{
		SendSubject:  cfg.NotifySendSubject,
		StateSubject: cfg.UsersStateSubject,
		BotID:        cfg.TwitchBotUserID,
	}, log.Named("reauth"))
	premium.SetReauthNotifier(reauth)
	standard.SetReauthNotifier(reauth)
	system.SetReauthNotifier(reauth)

	// Only the chat lanes ever process TypeClip (sesame routes !clip to
	// premium/standard by broadcaster tier, never to system), so only those
	// two need the fact publisher. Attaches before any consumer goroutine
	// starts, same as every other Set* above.
	premium.SetFactPublisher(pub)
	standard.SetFactPublisher(pub)

	d.startChatLanes(ctx, []bus.WeightedLane{
		{Sub: premiumSub, Subject: cfg.PremiumSubject, Handle: premium.Process, Reserve: cfg.PremiumReserve},
		{Sub: standardSub, Subject: cfg.StandardSubject, Handle: standard.Process},
	})
	d.startSystemLane(ctx, systemSub, system)

	// The client-scoped user.authorization.* subscriptions that feed the
	// notifier are ensured in the background below.
	closeStreamLane := d.startStreamLane(ctx, system)
	defer closeStreamLane()

	closeAuthzLane := d.startAuthzLane(ctx, system)
	defer closeAuthzLane()
	go system.EnsureClientEventSubs(ctx)

	fatalIf(log, rpc.SubscribeManage(nc, registry, tw, cfg.RPCPrefix, "outgress-rpc", nrApp, log.Named("rpc")),
		"failed to subscribe management rpc")

	// Channel-points reward management (create/edit/delete custom rewards under
	// each broadcaster's own token), driven synchronously by the dashboard tab.
	if err := rpc.SubscribeChannelPoints(nc, tw, cfg.RPCPrefix, "outgress-rpc", nrApp, log.Named("rpc")); err != nil {
		log.Fatal("failed to subscribe channel-points rpc", zap.Error(err))
	}

	// Chatter listing (Helix Get Chatters under the bot's user token), driven by
	// sesame's loyalty watch tick: one call per live channel per tick.
	fatalIf(log, rpc.SubscribeChatters(nc, tw, cfg.TwitchBotUserID, cfg.RPCPrefix, "outgress-rpc", nrApp, log.Named("rpc")),
		"failed to subscribe chatters rpc")
	fatalIf(log, bus.SubscribeRPCHealth(nc, serviceName, "outgress-rpc"), "failed to subscribe rpc health")

	health.Serve(env.Get("LISTEN_ADDR", ":8080"), serviceName, health.NATS("nats", nc))

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

// warnLocaleGaps logs one warning per supported locale that is missing keys, so
// a half-translated language shows up in the startup logs. Missing keys fall
// back to English at lookup time (i18n.T), so this never blocks startup; a
// declared locale with no catalog file yet reports its whole key set, capped for
// readability.
func warnLocaleGaps(log *zap.Logger) {
	for locale, missing := range i18n.Gaps() {
		if len(missing) == 0 {
			continue
		}
		log.Warn("i18n locale is missing keys; falling back to English",
			zap.String("locale", locale),
			zap.Int("missing_count", len(missing)),
			zap.Strings("missing_keys", capLocaleKeys(missing)))
	}
}

// capLocaleKeys bounds the key list logged for a locale gap so a single warning
// line stays readable when an entire catalog file is absent.
func capLocaleKeys(keys []string) []string {
	const maxKeys = 20
	if len(keys) > maxKeys {
		return keys[:maxKeys]
	}
	return keys
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

// creds is the Twitch app client id + secret every token grant presents.
func (d *deps) creds() twitch.ClientCredentials {
	return twitch.ClientCredentials{ID: d.cfg.TwitchClientID, Secret: d.cfg.TwitchClientSecret}
}

// newTwitchClient assembles the Helix client over the three token sources: the
// app token, the bot account's user token, and the per-broadcaster grants.
//
// The app and bot sources get a proactive background refresher (ctx is the
// process's root/service context from signal.NotifyContext in main, so both
// stop cleanly on shutdown): both are built once here and live for the whole
// process, so renewing them off the chat send path is pure upside -- see
// twitch.Source.StartBackgroundRefresh's doc for the ~320ms a lazy renewal
// would otherwise dump onto whichever chat message happens to observe
// expiry. Per-broadcaster sources get the equivalent treatment through a
// single cache-wide sweeper instead of one refresher per Source; see
// broadcasterTokens and twitch.BroadcasterTokens.StartRefreshSweep for why.
func (d *deps) newTwitchClient(ctx context.Context) *twitch.Client {
	appTokens := twitch.NewAppTokenSource(d.creds())
	appTokens.StartBackgroundRefresh(ctx)

	bot := d.botTokenSource()
	if bot != nil {
		bot.StartBackgroundRefresh(ctx)
	}

	broadcasterTokens := d.broadcasterTokens()
	broadcasterTokens.StartRefreshSweep(ctx)

	return twitch.NewClient(d.cfg.TwitchClientID, appTokens, bot, broadcasterTokens)
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
		return twitch.NewUserTokenSource(d.creds(), d.cfg.TwitchBotRefreshToken)
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
//
// No per-Source StartBackgroundRefresh here, unlike newTwitchClient's app
// and bot sources: a goroutine (and ticker) per cached broadcaster would
// mean up to maxBroadcasterSources = 2048 of them, plus eviction-tied
// cancellation plumbing to avoid leaking one per evicted Source. Instead,
// newTwitchClient starts twitch.BroadcasterTokens.StartRefreshSweep on the
// *twitch.BroadcasterTokens this returns: a single goroutine that walks the
// cache and renews whatever is due, which gets eviction handling for free
// (an evicted entry is just absent from the next pass). That sweep is what
// actually matters for a channel that is live: BroadcasterTokens.Get
// touches lastUsed on every cache hit, so an actively streaming channel's
// Source is never idle and never evicted by sourceIdleTTL (1h) -- it lives
// long enough to reach its own ~4h token expiry while still live, and
// without the sweep that renewal would land lazily on whatever chat message
// first observed it. Broadcaster tokens are additionally kept hot by the
// go-live pre-warm (internal/worker/tokenwarm.go), and any renewal --
// pre-warm, sweep, or lazy on-send -- is safe to run uncoordinated across
// replicas because mintOrAdopt (token.go) serializes real mints via the
// lease built in newMintLease below.
func (d *deps) broadcasterTokens() *twitch.BroadcasterTokens {
	return twitch.NewBroadcasterTokens(func(broadcasterID string) *twitch.Source {
		return d.storedTokenSource(broadcasterID, "")
	})
}

// storedTokenSource builds a user token source backed by the users-service
// token store for one account, seeded with an optional env refresh token.
// Every account gets a mint lease (see newMintLease) regardless of whether
// its Source ends up with a background refresher: the lease guards the mint
// itself, which both the bot's background-refresh path and a broadcaster's
// lazy on-send path can still trigger.
func (d *deps) storedTokenSource(accountID, seedRefresh string) *twitch.Source {
	store := tokenstore.New(d.nc, d.cfg.TokensSubjectPrefix, accountID)
	log := d.log
	return twitch.NewStoredUserTokenSource(d.creds(), seedRefresh, twitch.StoredTokenIO{
		Load: func(ctx context.Context) twitch.StoredLoad {
			loaded, err := store.Load(ctx)
			if err != nil {
				log.Debug("stored token unavailable", zap.String("account_id", accountID), zap.Error(err))
				return twitch.StoredLoad{}
			}
			return twitch.StoredLoad{
				RefreshToken:         loaded.RefreshToken,
				AccessToken:          loaded.AccessToken,
				AccessTokenExpiresAt: loaded.AccessTokenExpiresAt,
			}
		},
		Persist: func(ctx context.Context, access, refresh string, expiresAt time.Time) error {
			if err := store.Save(ctx, access, refresh, &expiresAt); err != nil {
				log.Warn("token persist failed", zap.String("account_id", accountID), zap.Error(err))
				return err
			}
			return nil
		},
	}, d.newMintLease(accountID))
}

// warmupTwitch pays the cold-start cost (token minting, DNS/TLS and the first
// HTTP/2 handshake) before consumers and readiness come online, instead of on
// the first real chat message handled by each new pod. A transient Twitch
// outage must not crash-loop the service, so the bounded warmup degrades to a
// warning.
func warmupTwitch(ctx context.Context, tw *twitch.Client, log *zap.Logger) {
	warmupStarted := time.Now()
	// tw.Warmup now mints up to two tokens (app, then bot) sequentially.
	// Measured 2026-08-20, id.twitch.tv cold TLS+request (network path only,
	// a trivial GET -- NOT a real grant, so Twitch-side grant processing is
	// not included): p50 310ms, max 430ms. Two of those worst-case is well
	// under a second even before any unmeasured grant-processing overhead,
	// so the pre-existing 8s budget stays generous with real margin to
	// spare even with the second mint added.
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

	coordinator := ratelimit.NewLeaseCoordinator(d.valkey, limiter, d.cfg.RateRegion, d.host,
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
	batch := worker.NewValkeyBatchStore(d.valkey)
	base := worker.Config{
		Limiter:  limiter,
		Registry: registry,
		Twitch:   tw,
		BotID:    d.cfg.TwitchBotUserID,
		Owner:    d.host,
		Conduit:  conduit.New(d.nc, d.cfg.ConduitSubject, d.cfg.TwitchConduitID, 60*time.Second, d.log.Named("conduit")),
		Batch:    batch,
		UserIDs:  worker.NewUserIDCache(),
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
	system.SetStreamInfoStore(projection.NewStore(d.valkey))

	return premium, standard, system, modVerifier.Close
}

// laneSubscribers connects the three lane subscribers; paced redelivery keeps
// rate-limit nacks from spinning.
func (d *deps) laneSubscribers() (premiumSub, standardSub, systemSub bus.Subscriber, closeAll func()) {
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
	_, err := bus.ConsumeWeighted(ctx, d.nrApp, lanes, bus.ScalePolicy{
		MinRoutines:    d.cfg.MinRoutines,
		MaxRoutines:    d.cfg.MaxRoutines,
		MaxConsumers:   d.cfg.MaxConsumers,
		ScaleUpAfter:   d.cfg.ScaleUpAfter,
		ScaleDownAfter: d.cfg.ScaleDownAfter,
	}, d.log)
	fatalIf(d.log, err, "failed to consume premium/standard lanes")
}

// startSystemLane keeps the system lane on its own independent consumer, off
// the weighted budget, so onboarding bursts never compete for the chat/api
// routines. It runs a fixed pool (min == max, single consumer), no autoscaling.
func (d *deps) startSystemLane(ctx context.Context, sub bus.Subscriber, system *worker.Worker) {
	_, err := bus.ConsumeWeighted(ctx, d.nrApp, []bus.WeightedLane{
		{Sub: sub, Subject: d.cfg.SystemSubject, Handle: system.Process},
	}, bus.ScalePolicy{
		MinRoutines:  d.cfg.SystemWorkers,
		MaxRoutines:  d.cfg.SystemWorkers,
		MaxConsumers: 1,
	}, d.log)
	fatalIf(d.log, err, "failed to consume system lane")
}

// startTokenWarmListener binds this replica's own core-NATS (non-queue)
// subscription to the projector's go-live token-warm fan-out (see
// outgress.TokenWarmScope and system.SubscribeTokenWarm). This is
// deliberately NOT a lane consumer: every one of the 3 outgress replicas
// needs its own subscription so every replica's independent
// twitch.BroadcasterTokens cache gets warmed, which a queue-grouped lane
// (where only one replica in the group ever dequeues a given message) cannot
// provide. Bound to the system worker because it already carries the
// takeSystemHelix budget the warm's Helix call spends from.
func (d *deps) startTokenWarmListener(system *worker.Worker) func() {
	sub, err := system.SubscribeTokenWarm(d.nc, d.cfg.CacheInvalidatePrefix)
	fatalIf(d.log, err, "failed to subscribe token-warm fan-out")
	return func() { _ = sub.Unsubscribe() }
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

// startAuthzLane binds one durable consumer per authorization lifecycle
// subject (granted / revoked / subrevoked) under outgress's service group, on
// the same TWITCH_INGRESS stream as the stream lane. Ingress publishes these
// when Twitch reports an authorization change; the system worker reconciles
// the channel's enrollment state (mark revoked, re-enroll on grant). Distinct
// literal subjects keep each handler's intent typed instead of re-dispatching
// on a wildcard.
func (d *deps) startAuthzLane(ctx context.Context, system *worker.Worker) func() {
	authzSub, err := bus.NewSubscriber(d.cfg.NATSURL, serviceName, d.log)
	fatalIf(d.log, err, "failed to connect authz subscriber")

	lanes := map[string]func(*bus.Message) error{
		d.cfg.AuthzGrantedSubject:    system.HandleAuthzGranted,
		d.cfg.AuthzRevokedSubject:    system.HandleAuthzRevoked,
		d.cfg.AuthzSubRevokedSubject: system.HandleAuthzSubRevoked,
	}
	for subject, handle := range lanes {
		fatalIf(d.log, bus.Consume(ctx, d.nrApp, authzSub, subject, handle, d.log),
			"failed to consume authz subject "+subject)
	}

	return func() { _ = authzSub.Close() }
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
