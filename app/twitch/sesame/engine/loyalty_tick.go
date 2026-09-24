// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"context"
	"math/rand/v2"
	"strconv"
	"strings"
	"sync"
	"time"

	"ItsBagelBot/app/twitch/sesame/module"
	"ItsBagelBot/internal/domain/invalidate"
	livekey "ItsBagelBot/internal/domain/live"
	"ItsBagelBot/internal/domain/outgress"
	"ItsBagelBot/internal/domain/rpc/manage"
	"ItsBagelBot/internal/projection"
	"ItsBagelBot/pkg/bus"
	"ItsBagelBot/pkg/cache"
	"ItsBagelBot/pkg/codec"
	pkg_valkey "ItsBagelBot/pkg/valkey"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nuid"
	"github.com/valkey-io/valkey-go"
	"go.uber.org/zap"
)

const (
	loyaltyTickKeyPrefix   = "loyaltick:"
	loyaltyTickClaimPrefix = "loyaltick:claim:"

	watchTickInterval = 5 * time.Minute

	watchTickJitter     = time.Minute
	loyaltyTickClaimTTL = 30 * time.Second

	// Must stay above outgress's 10s chattersHandleTimeout.
	chattersRPCTimeout = 12 * time.Second

	loyaltyRearmTimeout = 5 * time.Second

	watchTickQuickRetry   = time.Minute
	watchTickQuickRetries = 2

	watchTickReconfirmInterval = time.Hour
	loyaltyReconfirmKeyPrefix  = loyaltyTickClaimPrefix + "reconfirm:"

	loyaltyReconfirmTimeout = 5 * time.Second

	loyaltyEscalationLevel = 3

	loyaltyReconcileInterval = time.Minute
	loyaltyReconcileClaimKey = loyaltyTickClaimPrefix + "reconcile"
	loyaltyReconcileClaimTTL = 30 * time.Second
)

type ValkeyLoyaltyClock struct {
	client   valkey.Client
	proj     projection.Reader
	live     IsLiveChecker
	reporter *LoyaltyReporter

	nc              *nats.Conn
	request         func(context.Context, string, []byte) (*nats.Msg, error)
	chattersSubject string
	rearmSubject    string

	pub                   bus.Publisher
	outgressSystemSubject string

	botID      string
	keyspaceDB int
	log        *zap.Logger

	viewers *ValkeyChatters

	tmu      sync.Mutex
	failures map[uint64]int
}

type LoyaltyClockConfig struct {
	OutgressRPCPrefix        string
	ModulesInvalidateSubject string
	BotUserID                string
	KeyspaceDB               int
	Publisher                bus.Publisher
	OutgressSystemSubject    string
	ViewerSnapshots          *ValkeyChatters
	Log                      *zap.Logger
}

func NewValkeyLoyaltyClock(client valkey.Client, nc *nats.Conn, proj projection.Reader, live IsLiveChecker, reporter *LoyaltyReporter, cfg LoyaltyClockConfig) *ValkeyLoyaltyClock {
	log := cfg.Log
	if log == nil {
		log = zap.NewNop()
	}
	if cfg.OutgressRPCPrefix == "" {
		cfg.OutgressRPCPrefix = "bagel.rpc.outgress"
	}
	return &ValkeyLoyaltyClock{
		client:   client,
		proj:     proj,
		live:     live,
		reporter: reporter,
		nc:       nc,
		request: func(ctx context.Context, subject string, body []byte) (*nats.Msg, error) {
			return bus.RequestWithContext(ctx, nc, subject, body)
		},
		chattersSubject:       cfg.OutgressRPCPrefix + ".chatters.get",
		rearmSubject:          cfg.ModulesInvalidateSubject,
		pub:                   cfg.Publisher,
		outgressSystemSubject: cfg.OutgressSystemSubject,
		botID:                 cfg.BotUserID,
		keyspaceDB:            cfg.KeyspaceDB,
		viewers:               cfg.ViewerSnapshots,
		log:                   log,
		failures:              map[uint64]int{},
	}
}

func loyaltyTickKey(broadcasterID uint64) string {
	return cache.UserKey(loyaltyTickKeyPrefix, broadcasterID)
}

func (s *ValkeyLoyaltyClock) Arm(ctx context.Context, broadcasterID uint64) {
	if broadcasterID == 0 {
		return
	}
	if _, enabled := loyaltyModuleConfig(ctx, s.proj, broadcasterID); !enabled {
		return
	}
	offset := time.Duration(rand.Int64N(int64(watchTickJitter.Seconds())+1)) * time.Second
	err := s.client.Do(ctx, s.client.B().Set().Key(loyaltyTickKey(broadcasterID)).Value("1").Nx().
		ExSeconds(int64((watchTickInterval + offset).Seconds())).Build()).Error()
	if err != nil && !valkey.IsValkeyNil(err) {
		s.log.Warn("loyalty: failed to arm watch tick", module.BIDField(broadcasterID), zap.Error(err))
	}
}

func (s *ValkeyLoyaltyClock) Disarm(ctx context.Context, broadcasterID uint64) {
	if broadcasterID == 0 {
		return
	}
	s.tmu.Lock()
	delete(s.failures, broadcasterID)
	s.tmu.Unlock()
	if err := s.client.Do(ctx, s.client.B().Del().Key(loyaltyTickKey(broadcasterID)).Build()).Error(); err != nil {
		s.log.Warn("loyalty: failed to disarm watch tick", module.BIDField(broadcasterID), zap.Error(err))
	}
}

func (s *ValkeyLoyaltyClock) rearmIfLive(ctx context.Context, broadcasterID uint64) {
	if broadcasterID == 0 {
		return
	}
	live, err := s.live.IsLive(ctx, broadcasterID)
	if err != nil || !live {
		return
	}
	s.Arm(ctx, broadcasterID)
}

func (s *ValkeyLoyaltyClock) StartExpiryWatcher(ctx context.Context) {
	channel := "__keyevent@" + strconv.Itoa(s.keyspaceDB) + "__:expired"
	s.log.Info("loyalty: watch tick expiry watcher starting", zap.String("channel", channel))

	for ctx.Err() == nil {
		err := s.client.Receive(ctx, s.client.B().Subscribe().Channel(channel).Build(), func(msg valkey.PubSubMessage) {
			s.onExpired(ctx, msg.Message)
		})
		if ctx.Err() != nil {
			return
		}
		s.log.Warn("loyalty: expiry watcher dropped, reconnecting", zap.Error(err))
		select {
		case <-ctx.Done():
			return
		case <-time.After(time.Second):
		}
	}
}

func (s *ValkeyLoyaltyClock) StartRearmWatcher(ctx context.Context) {
	if s.nc == nil || s.rearmSubject == "" {
		return
	}
	sub, err := s.nc.Subscribe(s.rearmSubject, func(msg *nats.Msg) {
		var dto invalidate.DTO
		if err := codec.Unmarshal(msg.Data, &dto); err != nil {
			return
		}
		id, err := strconv.ParseUint(dto.BroadcasterID, 10, 64)
		if err != nil || id == 0 {
			return
		}
		go func() {
			rctx, cancel := context.WithTimeout(context.Background(), loyaltyRearmTimeout)
			defer cancel()
			s.rearmIfLive(rctx, id)
		}()
	})
	if err != nil {
		s.log.Error("loyalty: failed to start rearm watcher", zap.String("subject", s.rearmSubject), zap.Error(err))
		return
	}
	s.log.Info("loyalty: rearm watcher starting", zap.String("subject", s.rearmSubject))
	go func() {
		<-ctx.Done()
		_ = sub.Unsubscribe()
	}()
}

func (s *ValkeyLoyaltyClock) StartReconciler(ctx context.Context) {
	ticker := time.NewTicker(loyaltyReconcileInterval)
	defer ticker.Stop()
	s.log.Info("loyalty: reconciler starting", zap.Duration("interval", loyaltyReconcileInterval))
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.reconcile(ctx)
		}
	}
}

func (s *ValkeyLoyaltyClock) reconcile(ctx context.Context) {
	if won, err := pkg_valkey.ClaimOnce(ctx, s.client, loyaltyReconcileClaimKey, loyaltyReconcileClaimTTL); err != nil || !won {
		return
	}
	for _, id := range s.liveBroadcasters(ctx) {
		s.rearmIfLive(ctx, id)
	}
}

func (s *ValkeyLoyaltyClock) liveBroadcasters(ctx context.Context) []uint64 {
	var ids []uint64
	cursor := uint64(0)
	for {
		entry, err := s.client.Do(ctx, s.client.B().Scan().Cursor(cursor).Match(livekey.KeyPrefix+"*").Count(200).Build()).AsScanEntry()
		if err != nil {
			s.log.Warn("loyalty: reconcile scan failed", zap.Error(err))
			return ids
		}
		for _, k := range entry.Elements {
			if id, ok := parseLiveKey(k); ok {
				ids = append(ids, id)
			}
		}
		cursor = entry.Cursor
		if cursor == 0 {
			return ids
		}
	}
}

func parseLiveKey(key string) (uint64, bool) {
	if strings.HasPrefix(key, recheckKeyPrefix) {
		return 0, false
	}
	id, err := strconv.ParseUint(strings.TrimPrefix(key, livekey.KeyPrefix), 10, 64)
	return id, err == nil && id != 0
}

func (s *ValkeyLoyaltyClock) onExpired(ctx context.Context, key string) {
	if !strings.HasPrefix(key, loyaltyTickKeyPrefix) || strings.HasPrefix(key, loyaltyTickClaimPrefix) {
		return
	}
	broadcasterID, err := strconv.ParseUint(strings.TrimPrefix(key, loyaltyTickKeyPrefix), 10, 64)
	if err != nil || broadcasterID == 0 {
		return
	}
	go s.fire(ctx, broadcasterID)
}

func (s *ValkeyLoyaltyClock) fire(ctx context.Context, broadcasterID uint64) {
	claimKey := loyaltyTickClaimPrefix + strconv.FormatUint(broadcasterID, 10)
	if won, err := pkg_valkey.ClaimOnce(ctx, s.client, claimKey, loyaltyTickClaimTTL); err != nil || !won {
		return
	}

	live, err := s.live.IsLive(ctx, broadcasterID)
	if err != nil {
		s.rearmAfterFailure(ctx, broadcasterID, err)
		return
	}
	if !live {
		return
	}
	_, enabled := loyaltyModuleConfig(ctx, s.proj, broadcasterID)
	if !enabled {
		return
	}

	accrued, err := s.accrue(ctx, broadcasterID)
	if err != nil {
		s.rearmAfterFailure(ctx, broadcasterID, err)
		return
	}
	if accrued {
		s.rearm(ctx, broadcasterID, s.settleSuccess(ctx, broadcasterID))
	}
}

func (s *ValkeyLoyaltyClock) rearmAfterFailure(ctx context.Context, broadcasterID uint64, cause error) {
	s.rearm(ctx, broadcasterID, s.settleFailure(broadcasterID, cause))
}

func (s *ValkeyLoyaltyClock) rearm(ctx context.Context, broadcasterID uint64, ttl time.Duration) {
	err := s.client.Do(ctx, s.client.B().Set().Key(loyaltyTickKey(broadcasterID)).Value("1").Nx().
		ExSeconds(int64(ttl.Seconds())).Build()).Error()
	if err != nil && !valkey.IsValkeyNil(err) {
		s.log.Warn("loyalty: failed to re-arm watch tick", module.BIDField(broadcasterID), zap.Error(err))
	}
}

func (s *ValkeyLoyaltyClock) accrue(ctx context.Context, broadcasterID uint64) (bool, error) {
	chatters, err := s.fetchChatters(ctx, broadcasterID)
	if err != nil {
		return false, err
	}
	s.viewers.Store(ctx, broadcasterID, viewerSnapshotEntries(chatters))
	live, err := s.live.IsLive(ctx, broadcasterID)
	if err != nil || !live {
		return false, err
	}
	cfg, enabled := loyaltyModuleConfig(ctx, s.proj, broadcasterID)
	if !enabled {
		return false, nil
	}
	points := cfg.EffectiveWatchPointsPerTick()
	seconds := uint64(watchTickInterval.Seconds())
	seen := make(map[uint64]struct{}, len(chatters))
	for _, ch := range chatters {
		viewerID, ok := s.chatterViewerID(ch.ID)
		if !ok {
			continue
		}
		if _, duplicate := seen[viewerID]; duplicate {
			continue
		}
		seen[viewerID] = struct{}{}
		s.reporter.Earn(broadcasterID, viewerID, ch.Login, "", points, seconds)
	}
	s.log.Debug("loyalty: watch tick accrued",
		module.BIDField(broadcasterID), zap.Int("chatters", len(seen)))
	return true, nil
}

func (s *ValkeyLoyaltyClock) chatterViewerID(id string) (uint64, bool) {
	if id == s.botID {
		return 0, false
	}
	viewerID, err := strconv.ParseUint(id, 10, 64)
	return viewerID, err == nil && viewerID != 0
}

func (s *ValkeyLoyaltyClock) fetchChatters(ctx context.Context, broadcasterID uint64) ([]manage.Chatter, error) {
	ctx, cancel := context.WithTimeout(ctx, chattersRPCTimeout)
	defer cancel()

	body, err := codec.Marshal(manage.ChattersRequest{BroadcasterID: strconv.FormatUint(broadcasterID, 10)})
	if err != nil {
		return nil, err
	}
	msg, err := s.request(ctx, s.chattersSubject, body)
	if err != nil {
		return nil, err
	}
	var reply manage.ChattersReply
	if err := codec.Unmarshal(msg.Data, &reply); err != nil {
		return nil, err
	}
	if reply.Error != "" || reply.MissingScope {
		return nil, &chattersError{message: reply.Error, missingScope: reply.MissingScope}
	}
	return reply.Chatters, nil
}

type chattersError struct {
	message      string
	missingScope bool
}

func (e *chattersError) Error() string {
	if e.missingScope {
		return "chatters unavailable (missing scope or not a moderator): " + e.message
	}
	return e.message
}

func (s *ValkeyLoyaltyClock) settleSuccess(ctx context.Context, broadcasterID uint64) time.Duration {
	s.tmu.Lock()
	delete(s.failures, broadcasterID)
	s.tmu.Unlock()
	if s.pub != nil && s.outgressSystemSubject != "" {
		key := cache.UserKey(loyaltyReconfirmKeyPrefix, broadcasterID)
		lock := pkg_valkey.NewOwnerLock(s.client, key, nuid.Next())
		if won, err := lock.Acquire(ctx, watchTickReconfirmInterval); err == nil && won {
			if err := s.requestLiveRecheck(ctx, broadcasterID); err != nil {
				_ = lock.Release(ctx)
			}
		}
	}
	return watchTickInterval
}

func (s *ValkeyLoyaltyClock) settleFailure(broadcasterID uint64, cause error) time.Duration {
	s.tmu.Lock()
	n := s.failures[broadcasterID] + 1
	s.failures[broadcasterID] = n
	s.tmu.Unlock()

	fields := []zap.Field{
		module.BIDField(broadcasterID),
		zap.Int("consecutive_failures", n),
		zap.Error(cause),
	}
	if n < loyaltyEscalationLevel {
		s.log.Warn("loyalty: watch tick attempt failed", fields...)
	} else {
		s.log.Error("loyalty: watch tick keeps failing; check the bot grant's chatters scope or the channel's moderator seat", fields...)
	}
	return rearmAfterFailure(n)
}

func rearmAfterFailure(failures int) time.Duration {
	if failures <= watchTickQuickRetries {
		return watchTickQuickRetry
	}
	return watchTickInterval
}

func watchTickIdentity(broadcasterID uint64, at time.Time) string {
	buf := GetBuf()
	buf = append(buf, "wtick:"...)
	buf = strconv.AppendUint(buf, broadcasterID, 10)
	buf = append(buf, ':')
	buf = strconv.AppendInt(buf, at.Unix()/int64(watchTickInterval/time.Second), 10)
	key := string(buf)
	PutBuf(buf)
	return key
}

func (s *ValkeyLoyaltyClock) requestLiveRecheck(ctx context.Context, broadcasterID uint64) error {
	if s.pub == nil || s.outgressSystemSubject == "" {
		return nil
	}
	id := strconv.FormatUint(broadcasterID, 10)
	body, err := codec.Marshal(outgress.StreamStatusJob{BroadcasterID: id})
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, loyaltyReconfirmTimeout)
	defer cancel()
	if err := bus.PublishJSON(ctx, s.pub, s.outgressSystemSubject, outgress.Message{
		Type:          outgress.TypeStreamStatus,
		BroadcasterID: id,
		Payload:       body,
	}); err != nil {
		s.log.Debug("loyalty: live re-check publish failed",
			module.BIDField(broadcasterID), zap.Error(err))
		return err
	}
	return nil
}
