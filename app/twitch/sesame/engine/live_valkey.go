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
	"ItsBagelBot/internal/domain/invalidate"
	livekey "ItsBagelBot/internal/domain/live"
	"ItsBagelBot/internal/domain/outgress"
	projectorrpc "ItsBagelBot/internal/domain/rpc/projector"
	"ItsBagelBot/pkg/bus"
	"ItsBagelBot/pkg/cache"
	"ItsBagelBot/pkg/codec"

	"github.com/nats-io/nats.go"
	"github.com/valkey-io/valkey-go"
	"go.uber.org/zap"
)

const liveCacheCapacity int64 = 4096

type LiveConfig struct {
	TTL                   time.Duration
	CacheTTL              time.Duration
	ProjectorLiveSubject  string
	OutgressSystemSubject string
	CacheInvalidatePrefix string
	KeyspaceDB            int
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
	return s.cache.GetOrLoad(ctx, broadcasterID, func(ctx context.Context) (bool, error) {
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
	})
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
	channel := "__keyevent@" + strconv.Itoa(s.cfg.KeyspaceDB) + "__:expired"
	s.log.Info("live: expiry watcher starting", zap.String("channel", channel))

	for ctx.Err() == nil {
		err := s.client.Receive(ctx, s.client.B().Subscribe().Channel(channel).Build(), func(msg valkey.PubSubMessage) {
			s.onExpired(ctx, msg.Message)
		})
		if ctx.Err() != nil {
			return
		}
		s.log.Warn("live: expiry watcher dropped, reconnecting", zap.Error(err))
		select {
		case <-ctx.Done():
			return
		case <-time.After(time.Second):
		}
	}
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
	if confirmation && s.liveKeyExists(ctx, broadcasterID) {
		return
	}
	s.claimRecheck(ctx, recheckClaim{key: recheckKeyPrefix + suffix, window: expiryRecheckWindow, broadcasterID: id})
}

func (s *ValkeyLiveStore) requestRecheck(ctx context.Context, broadcasterID string) error {
	body, err := codec.Marshal(outgress.StreamStatusJob{BroadcasterID: broadcasterID})
	if err != nil {
		return err
	}
	return bus.PublishJSON(ctx, s.pub, s.cfg.OutgressSystemSubject, outgress.Message{
		Type:          outgress.TypeStreamStatus,
		BroadcasterID: broadcasterID,
		Payload:       body,
	})
}

func (s *ValkeyLiveStore) Close() {
	if s.invalidationSub != nil {
		_ = s.invalidationSub.Unsubscribe()
	}
	s.cache.Close()
}
