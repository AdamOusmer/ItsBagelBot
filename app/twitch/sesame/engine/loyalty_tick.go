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
	"ItsBagelBot/internal/projection"
	"ItsBagelBot/internal/watchtime"
	"ItsBagelBot/pkg/bus"
	"ItsBagelBot/pkg/cache"
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
	loyaltyReconcileInterval   = time.Minute
	loyaltyReconcileClaimKey   = loyaltyTickClaimPrefix + "reconcile"
	loyaltyReconcileClaimTTL   = 30 * time.Second
	loyaltyPollInterval        = time.Second
	loyaltyWorkers             = 4
	loyaltyQueueSize           = 64
	loyaltyStateRetention      = 30 * 24 * time.Hour
)

// The sorted set is the durable source of scheduled work. Expiry notifications
// only wake the bounded poller; a restart or dropped subscription cannot erase
// an outstanding window. Each worker handles ONE page, then yields the channel
// to the shared queue so large channels cannot occupy a worker for every page.
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
	rearmSubject    string
	botID           string
	keyspaceDB      int
	log             *zap.Logger
	wake            chan struct{}
	rearms          chan uint64
	now             func() time.Time
}

type LoyaltyClockConfig struct {
	OutgressRPCPrefix        string
	ModulesInvalidateSubject string
	BotUserID                string
	KeyspaceDB               int
	// Retained for rolling wiring compatibility. Reconfirmation now occurs in
	// the correlated chatter request and requires a successful Twitch result.
	Publisher             bus.Publisher
	OutgressSystemSubject string
	Log                   *zap.Logger
	ViewerSnapshots       *ValkeyChatters
}

func NewValkeyLoyaltyClock(client valkey.Client, nc *nats.Conn, proj projection.Reader, live IsLiveChecker, reporter *LoyaltyReporter, cfg LoyaltyClockConfig) *ValkeyLoyaltyClock {
	log := cfg.Log
	if log == nil {
		log = zap.NewNop()
	}
	if cfg.OutgressRPCPrefix == "" {
		cfg.OutgressRPCPrefix = "bagel.rpc.outgress"
	}
	primary := pkg_valkey.Primary(client)
	return &ValkeyLoyaltyClock{client: primary, proj: proj, live: live, reporter: reporter, awards: watchtime.NewStore(primary), viewers: cfg.ViewerSnapshots, nc: nc,
		request: func(ctx context.Context, subject string, body []byte) (*nats.Msg, error) {
			return bus.RequestWithContext(ctx, nc, subject, body)
		},
		chattersSubject: cfg.OutgressRPCPrefix + ".chatters.get", rearmSubject: cfg.ModulesInvalidateSubject, botID: cfg.BotUserID, keyspaceDB: cfg.KeyspaceDB,
		log: log, wake: make(chan struct{}, 1), rearms: make(chan uint64, loyaltyQueueSize), now: time.Now}
}

func loyaltyTickKey(id uint64) string { return cache.UserKey(loyaltyTickKeyPrefix, id) }

func loyaltyScheduleKey(id uint64) string { return cache.UserKey(loyaltySchedulePrefix, id) }

func loyaltyClaimKey(id uint64) string { return cache.UserKey(loyaltyTickClaimPrefix, id) }

// Arm is the recovery/legacy surface. An already active schedule stays intact;
// only a real versioned online event can start a new stream's window.

type loyaltyArmRequest struct {
	broadcaster uint64
	version     int64
	online      bool
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
	snap, allowed, err := s.awards.Capture(ctx, request.broadcaster)
	if err != nil {
		s.log.Warn("loyalty: admission unavailable while arming", module.BIDField(request.broadcaster), zap.Error(err))
		return false
	}
	if !allowed || snap.LiveSession == "" {
		return true
	}
	version, known, err := s.scheduleVersion(ctx, request)
	if err != nil {
		return false
	}
	if !known {
		return true
	}
	request.version = version
	return s.persistArm(ctx, request, snap.Generation)
}

func (s *ValkeyLoyaltyClock) persistArm(ctx context.Context, request loyaltyArmRequest, generation string) bool {
	offset := time.Duration(rand.Int64N(int64(watchTickJitter/time.Second)+1)) * time.Second
	due := s.now().Add(watchTickInterval + offset).UnixMilli()
	result, err := s.eval(ctx, loyaltyArmScript, []string{loyaltyScheduleKey(request.broadcaster), loyaltyDueKey, loyaltyClaimKey(request.broadcaster), livekey.Key(request.broadcaster)}, strconv.FormatUint(request.broadcaster, 10), strconv.FormatInt(request.version, 10), generation, strconv.FormatInt(request.version, 10), strconv.FormatInt(due, 10), request.mode())
	if err != nil {
		s.log.Warn("loyalty: failed to persist watch schedule", module.BIDField(request.broadcaster), zap.Error(err))
		return false
	}
	armed, err := result.AsInt64()
	if err != nil {
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
func (s *ValkeyLoyaltyClock) scheduleVersion(ctx context.Context, request loyaltyArmRequest) (int64, bool, error) {
	if request.version != 0 {
		return request.version, true, nil
	}
	raw, err := s.client.Do(ctx, s.client.B().Get().Key(livekey.Key(request.broadcaster)).Build()).ToString()
	if valkey.IsValkeyNil(err) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, err
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

func (s *ValkeyLoyaltyClock) eval(ctx context.Context, script string, keys []string, args ...string) (valkey.ValkeyResult, error) {
	result := s.client.Do(ctx, s.client.B().Eval().Script(script).Numkeys(int64(len(keys))).Key(keys...).Arg(args...).Build())
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
