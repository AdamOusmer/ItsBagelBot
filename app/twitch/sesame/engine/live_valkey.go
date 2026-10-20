// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"cmp"
	"context"
	"strconv"
	"strings"
	"time"

	"ItsBagelBot/app/twitch/sesame/module"
	"ItsBagelBot/internal/channelruntime"
	"ItsBagelBot/internal/domain/invalidate"
	livekey "ItsBagelBot/internal/domain/live"
	"ItsBagelBot/internal/domain/outgress"
	projectorrpc "ItsBagelBot/internal/domain/rpc/projector"
	"ItsBagelBot/pkg/bus"
	"ItsBagelBot/pkg/cache"
	"ItsBagelBot/pkg/codec"
	pkg_valkey "ItsBagelBot/pkg/valkey"

	"github.com/nats-io/nats.go"
	"github.com/valkey-io/valkey-go"
	"go.uber.org/zap"
)

const liveCacheCapacity int64 = 4096

const liveScanCount = 1000

type LiveConfig struct {
	Runtime               channelruntime.Reader
	TTL                   time.Duration
	CacheTTL              time.Duration
	ProjectorLiveSubject  string
	OutgressSystemSubject string
	CacheInvalidatePrefix string
	KeyspaceDB            int
	Expiry                *pkg_valkey.ExpiryHub
	OfflineConfirmDelays  []time.Duration
	Log                   *zap.Logger
}

type ValkeyLiveStore struct {
	client valkey.Client
	nc     *nats.Conn
	pub    bus.Publisher
	cfg    LiveConfig
	log    *zap.Logger

	cache      *cache.Keyed[uint64, bool]
	rpcTimeout time.Duration

	invalidationSub *nats.Subscription
}

func NewValkeyLiveStore(client valkey.Client, nc *nats.Conn, pub bus.Publisher, cfg LiveConfig) *ValkeyLiveStore {
	if cfg.CacheTTL <= 0 {
		cfg.CacheTTL = 30 * time.Second
	}
	if len(cfg.OfflineConfirmDelays) == 0 {
		cfg.OfflineConfirmDelays = defaultOfflineConfirmDelays
	}
	log := cfg.Log
	if log == nil {
		log = zap.NewNop()
	}
	return &ValkeyLiveStore{
		client:     client,
		nc:         nc,
		pub:        pub,
		cfg:        cfg,
		log:        log,
		cache:      cache.NewKeyed[uint64, bool](liveCacheCapacity, cfg.CacheTTL, livekey.Key),
		rpcTimeout: 1500 * time.Millisecond,
	}
}

func liveKey(id uint64) string { return livekey.Key(id) }

func (s *ValkeyLiveStore) IsLive(ctx context.Context, broadcasterID uint64) (bool, error) {
	if s.cfg.Runtime != nil {
		state, err := s.cfg.Runtime.Get(ctx, broadcasterID)
		if err != nil {
			return false, err
		}
		if !state.AllowsRuntime() {
			return false, nil
		}
	}
	return s.cache.GetOrLoad(ctx, broadcasterID, func(ctx context.Context) (bool, error) {
		return s.loadLive(ctx, broadcasterID)
	})
}

func (s *ValkeyLiveStore) loadLive(ctx context.Context, broadcasterID uint64) (bool, error) {
	_, err := s.client.Do(ctx, s.client.B().Get().Key(liveKey(broadcasterID)).Build()).ToString()
	if err == nil {
		return true, nil
	}
	if !valkey.IsValkeyNil(err) {
		return false, err
	}

	id := strconv.FormatUint(broadcasterID, 10)
	reply, err := bus.RequestJSONTimeout[projectorrpc.LiveReply](
		ctx, s.nc, s.cfg.ProjectorLiveSubject,
		projectorrpc.LiveRequest{BroadcasterID: id},
		s.rpcTimeout,
	)
	switch {
	case err != nil:
		return false, err
	case reply.Live:
		return s.setLiveKey(ctx, broadcasterID, cmp.Or(reply.Version, livekey.VersionNow()))
	case reply.Known:
		s.claimRecheck(ctx, recheckClaim{key: demandRecheckKeyPrefix + id, window: demandRecheckWindow, broadcasterID: id})
	}
	return false, nil
}

func (s *ValkeyLiveStore) SetLive(ctx context.Context, broadcasterID uint64, version int64) (bool, error) {
	applied, err := s.setLiveKey(ctx, broadcasterID, version)
	if err != nil || !applied {
		return false, err
	}
	s.cache.Set(broadcasterID, true)
	s.broadcast(broadcasterID)
	return true, nil
}

func (s *ValkeyLiveStore) ClearLive(ctx context.Context, broadcasterID uint64, version int64) (bool, error) {
	s.cache.Invalidate(broadcasterID)
	applied, err := clearLiveKey(ctx, s.client, broadcasterID, version)
	if applied {
		s.armOfflineConfirm(ctx, broadcasterID)
		s.broadcast(broadcasterID)
	}
	return applied, err
}

func (s *ValkeyLiveStore) setLiveKey(ctx context.Context, broadcasterID uint64, version int64) (bool, error) {
	ttl := s.cfg.TTL
	if ttl <= 0 {
		ttl = 12 * time.Hour
	}
	applied, err := setLiveScript.Exec(ctx, s.client, []string{liveKey(broadcasterID), livekey.VerKey(broadcasterID)}, []string{
		livekey.Value(version), strconv.FormatInt(int64(ttl.Seconds()), 10), strconv.FormatInt(int64(livekey.VerTTL.Seconds()), 10),
	}).AsInt64()
	if err != nil {
		return false, err
	}
	return applied > 0, nil
}

func clearLiveKey(ctx context.Context, client valkey.Client, broadcasterID uint64, version int64) (bool, error) {
	applied, err := clearLiveScript.Exec(ctx, client, []string{liveKey(broadcasterID), livekey.VerKey(broadcasterID)}, []string{
		livekey.Value(version), strconv.FormatInt(int64(livekey.VerTTL.Seconds()), 10),
	}).AsInt64()
	if err != nil {
		return false, err
	}
	return applied > 0, nil
}

// Not retryable: a retried DEL racing a concurrent SetLive could delete a freshly confirmed key.
var (
	setLiveScript   = valkey.NewLuaScript(livekey.SetScript)
	clearLiveScript = valkey.NewLuaScript(livekey.ClearScript)
)

func (s *ValkeyLiveStore) broadcast(broadcasterID uint64) {
	if s.nc == nil || s.cfg.CacheInvalidatePrefix == "" {
		return
	}
	if err := invalidate.Publish(s.nc, s.cfg.CacheInvalidatePrefix, livekey.InvalidateScope, strconv.FormatUint(broadcasterID, 10)); err != nil {
		s.log.Warn("live: failed to broadcast invalidation", module.BIDField(broadcasterID), zap.Error(err))
	}
}

func (s *ValkeyLiveStore) StartInvalidationListener() {
	if s.cfg.CacheInvalidatePrefix == "" {
		return
	}
	subject := s.cfg.CacheInvalidatePrefix + "." + livekey.InvalidateScope
	sub, err := s.nc.Subscribe(subject, func(msg *nats.Msg) {
		var dto invalidate.DTO
		if err := codec.Unmarshal(msg.Data, &dto); err != nil {
			return
		}
		id, err := strconv.ParseUint(dto.BroadcasterID, 10, 64)
		if err != nil || id == 0 {
			return
		}
		s.cache.Invalidate(id)
	})
	if err != nil {
		s.log.Error("live: failed to subscribe to invalidation", zap.String("subject", subject), zap.Error(err))
		return
	}
	s.invalidationSub = sub
	s.log.Info("live: invalidation listener started", zap.String("subject", subject))
}

func (s *ValkeyLiveStore) StartExpiryWatcher(ctx context.Context) {
	pkg_valkey.ExpiryHubOr(s.cfg.Expiry, s.client, s.cfg.KeyspaceDB, s.log).Watch(ctx, s.onExpired, livekey.KeyPrefix)
}

func (s *ValkeyLiveStore) onExpired(ctx context.Context, key string) {
	suffix, ok := strings.CutPrefix(key, livekey.KeyPrefix)
	if !ok {
		return
	}
	id := suffix
	staged, confirmation := strings.CutPrefix(key, offlineConfirmKeyPrefix)
	if confirmation {
		_, id, _ = strings.Cut(staged, ":")
	}
	broadcasterID, err := strconv.ParseUint(id, 10, 64)
	if err != nil || broadcasterID == 0 {
		return
	}
	if confirmation && !s.confirmationAllowed(ctx, broadcasterID) {
		return
	}
	s.claimRecheck(ctx, recheckClaim{key: recheckKeyPrefix + suffix, window: expiryRecheckWindow, broadcasterID: id})
}

func (s *ValkeyLiveStore) confirmationAllowed(ctx context.Context, broadcasterID uint64) bool {
	if s.liveKeyExists(ctx, broadcasterID) {
		return false
	}
	if s.cfg.Runtime == nil {
		return true
	}
	state, err := s.cfg.Runtime.Get(ctx, broadcasterID)
	if err != nil || !state.AllowsRuntime() {
		return false
	}
	id := strconv.FormatUint(broadcasterID, 10)
	fence, err := s.client.Do(ctx, s.client.B().Get().Key(offlineConfirmKeyPrefix+"admission:"+id).Build()).ToString()
	return err == nil && fence == runtimeFence(state)
}

func (s *ValkeyLiveStore) requestRecheck(ctx context.Context, broadcasterID string) error {
	var admission channelruntime.Snapshot
	if s.cfg.Runtime != nil {
		id, err := strconv.ParseUint(broadcasterID, 10, 64)
		if err != nil {
			return err
		}
		admission, err = s.cfg.Runtime.Get(ctx, id)
		if err != nil {
			return err
		}
		if !admission.AllowsRuntime() {
			return nil
		}
	}
	body, err := codec.Marshal(outgress.StreamStatusJob{BroadcasterID: broadcasterID})
	if err != nil {
		return err
	}
	return bus.PublishJSON(ctx, s.pub, s.cfg.OutgressSystemSubject, outgress.Message{
		AccountCreatedAt:    admission.AccountCreatedAt,
		AdmissionGeneration: admission.State.AdmissionGeneration,
		Type:                outgress.TypeStreamStatus,
		BroadcasterID:       broadcasterID,
		Payload:             body,
	})
}

func (s *ValkeyLiveStore) Close() {
	if s.invalidationSub != nil {
		_ = s.invalidationSub.Unsubscribe()
	}
	s.cache.Close()
}
