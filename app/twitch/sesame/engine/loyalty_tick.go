// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"context"
	"math/rand/v2"
	"strconv"
	"time"

	"ItsBagelBot/app/twitch/sesame/module"
	livekey "ItsBagelBot/internal/domain/live"
	loyaltyrpc "ItsBagelBot/internal/domain/rpc/loyalty"
	"ItsBagelBot/internal/projection"
	"ItsBagelBot/internal/watchtime"
	"ItsBagelBot/pkg/bus"
	"ItsBagelBot/pkg/cache"
	"ItsBagelBot/pkg/idempotency"
	pkg_valkey "ItsBagelBot/pkg/valkey"

	"github.com/nats-io/nats.go"
	"github.com/valkey-io/valkey-go"
	"go.uber.org/zap"
)

const (
	loyaltyTickKeyPrefix       = "loyaltick:"
	loyaltyTickClaimPrefix     = "loyaltick:claim:"
	loyaltySchedulePrefix      = "loyaltick:state:"
	loyaltyDueKey              = "loyaltick:due"
	loyaltyDiscoveryCursorKey  = "loyaltick:discovery:cursor"
	loyaltyDiscoveryQueueKey   = "loyaltick:discovery:queue"
	watchTickInterval          = 5 * time.Minute
	watchTickJitter            = time.Minute
	loyaltyTickClaimTTL        = 30 * time.Second
	chattersRPCTimeout         = 12 * time.Second
	loyaltyPageTimeout         = 20 * time.Second
	loyaltyRearmTimeout        = 5 * time.Second
	watchTickQuickRetry        = time.Minute
	watchTickQuickRetries      = 2
	watchTickReconfirmInterval = time.Hour
	watchCollectionMaxAge      = 10 * time.Minute
	loyaltyEscalationLevel     = 3
	loyaltyUnauthorizedPark    = 30 * time.Minute
	loyaltyReconcileInterval   = time.Minute
	loyaltyReconcileClaimKey   = loyaltyTickClaimPrefix + "reconcile"
	loyaltyReconcileClaimTTL   = 30 * time.Second
	loyaltyPollInterval        = time.Second
	loyaltyWorkers             = 4
	loyaltyQueueSize           = 64
	loyaltyStateRetention      = 30 * 24 * time.Hour
)

// Keep scheduled work in the sorted set; expiry notifications alone lose work on reconnect.
type ValkeyLoyaltyClock struct {
	client          valkey.Client
	proj            projection.Reader
	live            IsLiveChecker
	reporter        *LoyaltyReporter // retained constructor compatibility; watch awards use the outbox
	awards          *watchtime.Store
	viewers         *ValkeyChatters
	nc              *nats.Conn
	request         func(context.Context, string, []byte) (*nats.Msg, error)
	chattersSubject string
	invalidations   cache.Feed
	botID           string
	keyspaceDB      int
	expiry          *pkg_valkey.ExpiryHub
	log             *zap.Logger
	wake            chan struct{}
	rearms          chan uint64
	now             func() time.Time
	windowHealth    func(context.Context, uint64) (pkg_valkey.WindowHealth, error)
	watchStatus     func(context.Context) (loyaltyrpc.WatchStatus, error)
}

type LoyaltyClockConfig struct {
	Proj                  projection.Reader
	Live                  IsLiveChecker
	Reporter              *LoyaltyReporter
	OutgressRPCPrefix     string
	Invalidations         cache.Feed
	BotUserID             string
	KeyspaceDB            int
	Expiry                *pkg_valkey.ExpiryHub
	Publisher             bus.Publisher
	OutgressSystemSubject string
	Log                   *zap.Logger
	ViewerSnapshots       *ValkeyChatters
	WatchStatus           func(context.Context) (loyaltyrpc.WatchStatus, error)
}

func NewValkeyLoyaltyClock(client valkey.Client, nc *nats.Conn, cfg LoyaltyClockConfig) *ValkeyLoyaltyClock {
	proj, live, reporter := cfg.Proj, cfg.Live, cfg.Reporter
	log := cfg.Log
	if log == nil {
		log = zap.NewNop()
	}
	if cfg.OutgressRPCPrefix == "" {
		cfg.OutgressRPCPrefix = "rpc.outgress"
	}
	primary := pkg_valkey.Primary(client)
	return &ValkeyLoyaltyClock{client: primary, proj: proj, live: live, reporter: reporter, awards: watchtime.NewStore(primary), viewers: cfg.ViewerSnapshots, nc: nc,
		request: func(ctx context.Context, subject string, body []byte) (*nats.Msg, error) {
			return bus.RequestWithContext(ctx, nc, subject, body)
		},
		chattersSubject: cfg.OutgressRPCPrefix + ".chatters.get", invalidations: cfg.Invalidations, botID: cfg.BotUserID, keyspaceDB: cfg.KeyspaceDB, expiry: cfg.Expiry,
		log: log, wake: make(chan struct{}, 1), rearms: make(chan uint64, loyaltyQueueSize), now: time.Now,
		watchStatus: cfg.WatchStatus, windowHealth: func(ctx context.Context, id uint64) (pkg_valkey.WindowHealth, error) {
			return pkg_valkey.ReadWindowHealth(ctx, primary, loyaltyScheduleKey(id))
		}}
}

func loyaltyTickKey(id uint64) string { return cache.UserKey(loyaltyTickKeyPrefix, id) }

func loyaltyScheduleKey(id uint64) string { return cache.UserKey(loyaltySchedulePrefix, id) }

func loyaltyClaimKey(id uint64) string { return cache.UserKey(loyaltyTickClaimPrefix, id) }

type loyaltyArmRequest struct {
	broadcaster uint64
	version     int64
	online      bool
	liveSession int64
}

func (s *ValkeyLoyaltyClock) Arm(ctx context.Context, id uint64) {
	s.arm(ctx, loyaltyArmRequest{broadcaster: id})
}
func (s *ValkeyLoyaltyClock) ArmVersioned(ctx context.Context, id uint64, version int64) {
	s.arm(ctx, loyaltyArmRequest{broadcaster: id, version: version, online: true})
}

func (s *ValkeyLoyaltyClock) arm(ctx context.Context, request loyaltyArmRequest) bool {
	if request.broadcaster == 0 {
		return true
	}
	liveSession, known, err := s.liveSession(ctx, request)
	if err != nil {
		idempotency.Fail(ctx, err)
		return false
	}
	if !known {
		return true
	}
	snap, allowed, err := s.awards.Capture(ctx, request.broadcaster)
	if err != nil {
		idempotency.Fail(ctx, err)
		s.log.Warn("loyalty: admission unavailable while arming", module.BIDField(request.broadcaster), zap.Error(err))
		return false
	}
	if !allowed || snap.LiveSession == "" {
		return true
	}
	request.liveSession = liveSession
	if request.version == 0 {
		request.version = liveSession
	}
	return s.persistArm(ctx, request, snap.Generation)
}

func (s *ValkeyLoyaltyClock) persistArm(ctx context.Context, request loyaltyArmRequest, generation string) bool {
	offset := time.Duration(rand.Int64N(int64(watchTickJitter/time.Second)+1)) * time.Second
	due := s.now().Add(watchTickInterval + offset).UnixMilli()
	result, err := s.eval(ctx, loyaltyArmScript, []string{loyaltyScheduleKey(request.broadcaster), loyaltyDueKey, loyaltyClaimKey(request.broadcaster), livekey.Key(request.broadcaster)}, strconv.FormatUint(request.broadcaster, 10), strconv.FormatInt(request.version, 10), generation, strconv.FormatInt(request.liveSession, 10), strconv.FormatInt(due, 10), request.mode())
	if err != nil {
		s.log.Warn("loyalty: failed to persist watch schedule", module.BIDField(request.broadcaster), zap.Error(err))
		return false
	}
	armed, err := result.AsInt64()
	if err != nil {
		idempotency.Fail(ctx, err)
		return false
	}
	if armed == 1 {
		s.nudge()
	}
	return true
}
func (request loyaltyArmRequest) mode() string {
	if request.online {
		return "online"
	}
	return "recover"
}
func (s *ValkeyLoyaltyClock) liveSession(ctx context.Context, request loyaltyArmRequest) (int64, bool, error) {
	raw, ok, err := pkg_valkey.ReadString(ctx, s.client, livekey.Key(request.broadcaster))
	if err != nil {
		return 0, false, err
	}
	if !ok {
		return 0, false, nil
	}
	version, err := strconv.ParseInt(raw, 10, 64)
	return version, err == nil, nil
}

func (s *ValkeyLoyaltyClock) Disarm(ctx context.Context, id uint64) {
	s.DisarmVersioned(ctx, id, livekey.VersionNow())
}

func (s *ValkeyLoyaltyClock) DisarmVersioned(ctx context.Context, id uint64, version int64) {
	if id == 0 {
		return
	}
	if version == 0 {
		version = livekey.VersionNow()
	}
	_, err := s.eval(ctx, loyaltyDisarmScript, []string{loyaltyScheduleKey(id), loyaltyDueKey, loyaltyTickKey(id), loyaltyClaimKey(id)},
		strconv.FormatUint(id, 10), strconv.FormatInt(version, 10), strconv.FormatInt(loyaltyStateRetention.Milliseconds(), 10))
	if err != nil {
		s.log.Warn("loyalty: failed to stop watch schedule", module.BIDField(id), zap.Error(err))
	}
}

func (s *ValkeyLoyaltyClock) eval(ctx context.Context, script pkg_valkey.Script, keys []string, args ...string) (valkey.ValkeyResult, error) {
	result := script.Exec(ctx, s.client, keys, args)
	idempotency.Fail(ctx, result.Error())
	return result, result.Error()
}

func rearmAfterFailure(failures int) time.Duration {
	if failures <= watchTickQuickRetries {
		return watchTickQuickRetry
	}
	return watchTickInterval
}

func watchTickIdentity(id uint64, at time.Time) string {
	return "wtick:" + strconv.FormatUint(id, 10) + ":" + strconv.FormatInt(at.Unix()/int64(watchTickInterval/time.Second), 10)
}
