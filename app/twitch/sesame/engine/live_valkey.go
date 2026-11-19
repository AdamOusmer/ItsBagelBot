// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"cmp"
	"context"
	"strconv"
	"strings"
	"time"

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
	Invalidations         *bus.Invalidations
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
}

func NewValkeyLiveStore(client valkey.Client, nc *nats.Conn, pub bus.Publisher, cfg LiveConfig) *ValkeyLiveStore {
	if cfg.Invalidations == nil {
		cfg.Invalidations = bus.NewInvalidations(nil, "", cfg.Log)
	}
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
		cache:      cache.NewKeyed[uint64, bool](liveCacheCapacity, cfg.CacheTTL, livekey.Key).InvalidateOn(cfg.Invalidations, func(e cache.Event) []uint64 { return []uint64{e.ID} }, invalidate.ScopeLive),
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
	_, ok, err := pkg_valkey.NewKV(s.client).ReadBytes(ctx, liveKey(broadcasterID))
	if err != nil {
		return false, err
	}
	if ok {
		return true, nil
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
	s.cfg.Invalidations.Publish(invalidate.ScopeLive, broadcasterID)
	return true, nil
}

func (s *ValkeyLiveStore) ClearLive(ctx context.Context, broadcasterID uint64, version int64) (bool, error) {
	s.cache.Invalidate(broadcasterID)
	applied, err := clearLiveKey(ctx, s.client, broadcasterID, version)
	if applied {
		s.armOfflineConfirm(ctx, broadcasterID)
		s.cfg.Invalidations.Publish(invalidate.ScopeLive, broadcasterID)
	}
	return applied, err
}

func (s *ValkeyLiveStore) setLiveKey(ctx context.Context, broadcasterID uint64, version int64) (bool, error) {
	ttl := s.cfg.TTL
	if ttl <= 0 {
		ttl = 12 * time.Hour
	}
	return liveSlot(broadcasterID).Set(ctx, s.client, pkg_valkey.SlotWrite{Version: version, TTL: ttl, VerTTL: livekey.VerTTL})
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
	fence, ok := pkg_valkey.NewKV(s.client).GetString(ctx, offlineConfirmKeyPrefix+"admission:"+id)
	return ok && fence == runtimeFence(state)
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
	s.cache.Close()
}
