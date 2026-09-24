// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package main

import (
	"context"
	"os"
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
	"ItsBagelBot/pkg/ratelimit"
	"ItsBagelBot/pkg/svcboot"
	pkg_valkey "ItsBagelBot/pkg/valkey"

	"github.com/nats-io/nats.go"
	"github.com/newrelic/go-agent/v3/newrelic"
	valkey_go "github.com/valkey-io/valkey-go"

	"go.uber.org/zap"
)

const (
	serviceName = "outgress"
	queueGroup  = "outgress-rpc"
)

const (
	nakDelay        = time.Second
	maxRedeliveries = 3

	systemNakDelay        = 15 * time.Second
	systemMaxRedeliveries = 6

	twitchWarmupTimeout = 8 * time.Second
	conduitCacheTTL     = 60 * time.Second
)

type deps struct {
	core   svcboot.Core
	cfg    *config.Config
	log    *zap.Logger
	nrApp  *newrelic.Application
	nc     *nats.Conn
	valkey valkey_go.Client
	host   string
}

func main() {
	core, done := svcboot.NewCore(serviceName)
	defer done()
	log, ctx, nrApp := core.Log, core.Ctx, core.NR

	cfg := config.Load()
	warnStartupFallbacks(cfg, log)
	i18n.WarnGaps(log)

	svcboot.FatalIf(log, bus.EnsureStreams(ctx, cfg.NATSURL, []bus.StreamSpec{bus.OutgressStream, bus.OutgressSystemStream}, log),
		"failed to provision outgress streams")

	valkeyClient, err := pkg_valkey.NewOptionalClient(core.ValkeyAddr, core.ValkeyPassword)
	svcboot.FatalIf(log, err, "invalid optional Valkey configuration")
	defer valkeyClient.Close()

	activity.SetSink(activity.NewStore(valkeyClient))

	registry := channels.New(valkeyClient)

	nc := svcboot.MustRPCConn(core, cfg.NATSRPCURL)
	defer nc.Close()
	defer registry.Close()

	pub, err := bus.NewPublisher(cfg.NATSURL, log)
	svcboot.FatalIf(log, err, "failed to connect publisher")
	defer func() { _ = pub.Close() }()

	host := podIdentity(log)
	worker.SetNodeIdentity(cfg.RateRegion, env.Get("NODE_NAME", host))

	d := &deps{core: core, cfg: cfg, log: log, nrApp: nrApp, nc: nc, valkey: valkeyClient, host: host}

	tw := d.newTwitchClient(ctx)
	defer tw.CloseIdleConnections()
	warmupTwitch(ctx, tw, log)

	limiter, batch, closeCoordination := d.newSendingCoordination(ctx, registry)
	defer closeCoordination()
	svcboot.FatalIf(log, registry.StartInvalidationListener(nc, cfg.CacheInvalidatePrefix, log.Named("channels")),
		"failed to subscribe channel cache invalidation")

	premium, standard, system, closeWorkers := d.newLaneWorkers(tw, limiter, registry, batch)
	defer closeWorkers()

	closeTokenWarm := d.startTokenWarmListener(system)
	defer closeTokenWarm()

	premiumSub, standardSub, systemSub, closeSubs := d.laneSubscribers()
	defer closeSubs()

	reauth := worker.NewReauthNotifier(nc, worker.ReauthConfig{
		SendSubject:   cfg.NotifySendSubject,
		StateSubject:  cfg.UsersStateSubject,
		ActiveSubject: cfg.UsersActiveSubject,
		BotID:         cfg.TwitchBotUserID,
	}, log.Named("reauth"))
	premium.SetReauthNotifier(reauth)
	standard.SetReauthNotifier(reauth)
	system.SetReauthNotifier(reauth)

	premium.SetFactPublisher(pub)
	standard.SetFactPublisher(pub)

	d.startChatLanes(ctx, []bus.WeightedLane{
		{Sub: premiumSub, Subject: cfg.PremiumSubject, Handle: premium.Process, Reserve: cfg.PremiumReserve},
		{Sub: standardSub, Subject: cfg.StandardSubject, Handle: standard.Process},
	})
	d.startSystemLane(ctx, systemSub, system)

	closeStreamLane := d.startStreamLane(ctx, system)
	defer closeStreamLane()

	closeAuthzLane := d.startAuthzLane(ctx, system)
	defer closeAuthzLane()
	go system.EnsureClientEventSubs(ctx)

	svcboot.FatalIf(log, rpc.SubscribeManage(nc, registry, tw, cfg.RPCPrefix, queueGroup, nrApp, log.Named("rpc")),
		"failed to subscribe management rpc")
	svcboot.FatalIf(log, rpc.SubscribeTrialSubscriptions(bus.RPCWiring{
		NC: nc, App: nrApp, Queue: "outgress-trial-subscription", Log: log.Named("trial-rpc"), Timeout: 5 * time.Second,
	}, valkeyClient, tw, cfg.TwitchBotUserID),
		"failed to subscribe trial subscription rpc")

	svcboot.FatalIf(log, rpc.SubscribeChannelPoints(nc, tw, cfg.RPCPrefix, queueGroup, nrApp, log.Named("rpc")),
		"failed to subscribe channel-points rpc")

	svcboot.FatalIf(log, rpc.SubscribeChatters(nc, tw, cfg.TwitchBotUserID, cfg.RPCPrefix, queueGroup, nrApp, log.Named("rpc")),
		"failed to subscribe chatters rpc")
	d.serveHealth(premiumSub, standardSub, systemSub)

	d.logReady(tw)

	core.Await()
}

func warnStartupFallbacks(cfg *config.Config, log *zap.Logger) {
	if os.Getenv("OUTGRESS_REGION") == "" {
		log.Warn("OUTGRESS_REGION is unset; using fallback locality",
			zap.String("rate_region", cfg.RateRegion))
	}
	if err := worker.PrepareJSON(); err != nil {
		log.Warn("failed to precompile outgress JSON decoders", zap.Error(err))
	}
}

func podIdentity(log *zap.Logger) string {
	host, err := os.Hostname()
	if err != nil || host == "" {
		log.Fatal("failed to determine outgress pod identity", zap.Error(err))
	}
	return host
}

func (d *deps) creds() twitch.ClientCredentials {
	return twitch.ClientCredentials{ID: d.cfg.TwitchClientID, Secret: d.cfg.TwitchClientSecret}
}

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

func (d *deps) broadcasterTokens() *twitch.BroadcasterTokens {
	return twitch.NewBroadcasterTokens(func(broadcasterID string) *twitch.Source {
		return d.storedTokenSource(broadcasterID, "")
	})
}

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

func warmupTwitch(ctx context.Context, tw *twitch.Client, log *zap.Logger) {
	warmupStarted := time.Now()
	warmupCtx, warmupCancel := context.WithTimeout(ctx, twitchWarmupTimeout)
	err := tw.Warmup(warmupCtx)
	warmupCancel()
	if err != nil {
		log.Warn("twitch warmup failed; continuing with lazy retry",
			zap.Duration("duration", time.Since(warmupStarted)), zap.Error(err))
		return
	}
	log.Info("twitch client warmed", zap.Duration("duration", time.Since(warmupStarted)))
}

func (d *deps) newLaneWorkers(tw *twitch.Client, limiter ratelimit.Manager, registry *channels.Registry, batch worker.BatchStore) (premium, standard, system *worker.Worker, cleanup func()) {
	base := worker.Config{
		Limiter:  limiter,
		Registry: registry,
		Twitch:   tw,
		BotID:    d.cfg.TwitchBotUserID,
		Owner:    d.host,
		Conduit:  conduit.New(d.nc, d.cfg.ConduitSubject, d.cfg.TwitchConduitID, conduitCacheTTL, d.log.Named("conduit")),
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

func (d *deps) laneSubscribers() (premiumSub, standardSub, systemSub bus.Subscriber, closeAll func()) {
	var err error
	premiumSub, err = bus.NewLaneSubscriber(bus.LaneConfig{
		URL: d.cfg.NATSURL, Stream: bus.OutgressStream.Name, Subject: d.cfg.PremiumSubject,
		Group: "outgress-premium", NakDelay: nakDelay, MaxRedeliveries: maxRedeliveries,
	}, d.log)
	svcboot.FatalIf(d.log, err, "failed to connect premium subscriber")

	standardSub, err = bus.NewLaneSubscriber(bus.LaneConfig{
		URL: d.cfg.NATSURL, Stream: bus.OutgressStream.Name, Subject: d.cfg.StandardSubject,
		Group: "outgress-standard", NakDelay: nakDelay, MaxRedeliveries: maxRedeliveries,
	}, d.log)
	svcboot.FatalIf(d.log, err, "failed to connect standard subscriber")

	systemSub, err = bus.NewLaneSubscriber(bus.LaneConfig{
		URL: d.cfg.NATSURL, Stream: bus.OutgressSystemStream.Name, Subject: d.cfg.SystemSubject,
		Group: "outgress-system", NakDelay: systemNakDelay, MaxRedeliveries: systemMaxRedeliveries,
	}, d.log)
	svcboot.FatalIf(d.log, err, "failed to connect system subscriber")

	return premiumSub, standardSub, systemSub, func() {
		_ = systemSub.Close()
		_ = standardSub.Close()
		_ = premiumSub.Close()
	}
}

func (d *deps) serveHealth(premiumSub, standardSub, systemSub bus.Subscriber) {
	svcboot.ServeHealth(svcboot.Health{
		Log: d.log, NC: d.nc, Service: serviceName, QueueGroup: queueGroup, ListenAddr: d.core.ListenAddr,
	},
		bus.LaneCheck("premium", premiumSub),
		bus.LaneCheck("standard", standardSub),
		bus.LaneCheck("system", systemSub),
	)
}

func (d *deps) startChatLanes(ctx context.Context, lanes []bus.WeightedLane) {
	_, err := bus.ConsumeWeighted(ctx, d.nrApp, lanes, bus.ScalePolicy{
		MinRoutines:    d.cfg.MinRoutines,
		MaxRoutines:    d.cfg.MaxRoutines,
		MaxConsumers:   d.cfg.MaxConsumers,
		ScaleUpAfter:   d.cfg.ScaleUpAfter,
		ScaleDownAfter: d.cfg.ScaleDownAfter,
	}, d.log)
	svcboot.FatalIf(d.log, err, "failed to consume premium/standard lanes")
}

func (d *deps) startSystemLane(ctx context.Context, sub bus.Subscriber, system *worker.Worker) {
	_, err := bus.ConsumeWeighted(ctx, d.nrApp, []bus.WeightedLane{
		{Sub: sub, Subject: d.cfg.SystemSubject, Handle: system.Process},
	}, bus.ScalePolicy{
		MinRoutines:  d.cfg.SystemWorkers,
		MaxRoutines:  d.cfg.SystemWorkers,
		MaxConsumers: 1,
	}, d.log)
	svcboot.FatalIf(d.log, err, "failed to consume system lane")
}

func (d *deps) startTokenWarmListener(system *worker.Worker) func() {
	sub, err := system.SubscribeTokenWarm(d.nc, d.cfg.CacheInvalidatePrefix)
	svcboot.FatalIf(d.log, err, "failed to subscribe token-warm fan-out")
	return func() { _ = sub.Unsubscribe() }
}

func (d *deps) startStreamLane(ctx context.Context, system *worker.Worker) func() {
	streamSub, err := bus.NewSubscriber(d.cfg.NATSURL, serviceName, d.log)
	svcboot.FatalIf(d.log, err, "failed to connect stream-lane subscriber")

	svcboot.FatalIf(d.log, bus.Consume(ctx, d.nrApp, streamSub, d.cfg.StreamLaneSubject, system.HandleStreamEvent, d.log),
		"failed to consume stream lane")

	return func() { _ = streamSub.Close() }
}

func (d *deps) startAuthzLane(ctx context.Context, system *worker.Worker) func() {
	authzSub, err := bus.NewSubscriber(d.cfg.NATSURL, serviceName, d.log)
	svcboot.FatalIf(d.log, err, "failed to connect authz subscriber")

	lanes := map[string]func(*bus.Message) error{
		d.cfg.AuthzGrantedSubject:    system.HandleAuthzGranted,
		d.cfg.AuthzRevokedSubject:    system.HandleAuthzRevoked,
		d.cfg.AuthzSubRevokedSubject: system.HandleAuthzSubRevoked,
	}
	for subject, handle := range lanes {
		svcboot.FatalIf(d.log, bus.Consume(ctx, d.nrApp, authzSub, subject, handle, d.log),
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
