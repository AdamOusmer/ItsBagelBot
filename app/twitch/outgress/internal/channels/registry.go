// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package channels

import (
	"context"
	"errors"
	"math/rand/v2"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"ItsBagelBot/internal/domain/invalidate"
	"ItsBagelBot/internal/domain/rpc/manage"
	"ItsBagelBot/internal/utils"
	"ItsBagelBot/pkg/cache"
	"ItsBagelBot/pkg/codec"
	"ItsBagelBot/pkg/kvstate"
	pkg_valkey "ItsBagelBot/pkg/valkey"

	"github.com/nats-io/nats.go"
	"github.com/valkey-io/valkey-go"
	"go.uber.org/zap"
)

const (
	keyPrefix        = "outgress:channel:"
	indexKey         = "outgress:channels"
	pausedKey        = "outgress:paused"
	pausedVersionKey = "outgress:paused:version"

	unhealthyKey          = "outgress:channels:unhealthy"
	unhealthySweepLockKey = "outgress:channels:unhealthy:sweep"

	cacheInvalidateScope = "outgress"
	pauseInvalidateScope = "outgress-pause"
)

const (
	pauseReconcileInterval = time.Second
	pauseReconcileJitter   = 200 * time.Millisecond
	pauseMaxAge            = 5 * time.Second
	pauseReadTimeout       = 750 * time.Millisecond
)

const (
	channelCacheCapacity = 4096
	channelCacheTTL      = 6 * time.Hour
)

var ErrPauseStateUnavailable = errors.New("outgress pause state is unavailable or stale")

type pauseSnapshot struct {
	paused     bool
	version    int64
	observedAt time.Time
}

type pauseEvent struct {
	Paused  bool  `json:"paused"`
	Version int64 `json:"version"`
}

type Registry struct {
	pauseStore kvstate.Store
	client     valkey.Client
	cache      *cache.Cache[manage.Channel]

	nc               *nats.Conn
	invalidatePrefix string
	invalidateSub    *nats.Subscription
	pauseSub         *nats.Subscription
	log              *zap.Logger

	pause       atomic.Pointer[pauseSnapshot]
	pauseCancel context.CancelFunc
	pauseWG     sync.WaitGroup
}

func New(client valkey.Client) *Registry {
	return &Registry{
		client: pkg_valkey.Primary(client),
		cache:  cache.New[manage.Channel](channelCacheCapacity, channelCacheTTL),
	}
}

func (r *Registry) StartInvalidationListener(nc *nats.Conn, prefix string, log *zap.Logger) error {
	r.nc = nc
	r.invalidatePrefix = prefix
	r.log = log

	sub, err := r.subscribeChannelInvalidation(prefix, log)
	if err != nil {
		return err
	}

	pauseSub, err := r.subscribePauseInvalidation(prefix, log)
	if err != nil {
		_ = sub.Unsubscribe()
		return err
	}

	if err := r.seedPauseSnapshot(); err != nil {
		_ = sub.Unsubscribe()
		_ = pauseSub.Unsubscribe()
		return err
	}

	r.invalidateSub = sub
	r.pauseSub = pauseSub
	pollCtx, cancel := context.WithCancel(context.Background())
	r.pauseCancel = cancel
	r.pauseWG.Add(1)
	go r.reconcilePause(pollCtx)
	return nil
}

func (r *Registry) subscribeChannelInvalidation(prefix string, log *zap.Logger) (*nats.Subscription, error) {
	return r.nc.Subscribe(prefix+"."+cacheInvalidateScope, func(msg *nats.Msg) {
		var payload invalidate.DTO
		if err := codec.Unmarshal(msg.Data, &payload); err != nil {
			log.Debug("channel cache invalidation: bad payload", zap.Error(err))
			return
		}
		if payload.BroadcasterID == "" {
			return
		}
		r.cache.Invalidate(payload.BroadcasterID)
	})
}

func (r *Registry) subscribePauseInvalidation(prefix string, log *zap.Logger) (*nats.Subscription, error) {
	return r.nc.Subscribe(prefix+"."+pauseInvalidateScope, func(msg *nats.Msg) {
		if r.pauseStore != nil {
			return
		}
		var event pauseEvent
		if err := codec.Unmarshal(msg.Data, &event); err != nil || event.Version < 1 {
			log.Debug("pause cache invalidation: bad payload", zap.Error(err))
			return
		}
		r.applyPauseSnapshot(pauseSnapshot{
			paused:     event.Paused,
			version:    event.Version,
			observedAt: time.Now(),
		})
	})
}

// Call only after subscribing, or this read can revert a concurrent pause.
func (r *Registry) seedPauseSnapshot() error {
	if err := r.nc.Flush(); err != nil {
		return err
	}
	initialCtx, initialCancel := context.WithTimeout(context.Background(), pauseReadTimeout)
	initial, err := r.loadPauseSnapshot(initialCtx)
	initialCancel()
	if err != nil {
		return err
	}
	r.applyPauseSnapshot(initial)
	return nil
}

func (r *Registry) Close() {
	if r.pauseCancel != nil {
		r.pauseCancel()
		r.pauseWG.Wait()
	}
	if r.invalidateSub != nil {
		_ = r.invalidateSub.Unsubscribe()
	}
	if r.pauseSub != nil {
		_ = r.pauseSub.Unsubscribe()
	}
	r.cache.Close()
}

func (r *Registry) publishInvalidation(broadcasterID string) {
	if r.nc == nil || r.invalidatePrefix == "" {
		return
	}
	if err := invalidate.Publish(r.nc, r.invalidatePrefix, cacheInvalidateScope, broadcasterID); err != nil && r.log != nil {
		r.log.Warn("failed to publish channel cache invalidation",
			zap.String("broadcaster_id", broadcasterID), zap.Error(err))
	}
}

func channelFromFields(broadcasterID string, fields map[string]string) manage.Channel {
	if len(fields) == 0 {
		return manage.Channel{}
	}
	return manage.Channel{
		BroadcasterID:  broadcasterID,
		Enabled:        fields["enabled"] == "1",
		IsMod:          fields["is_mod"] == "1",
		ModCheckedAt:   unixField(fields["mod_checked_at"]),
		UpdatedAt:      unixField(fields["updated_at"]),
		SubState:       fields["sub_state"],
		SubError:       fields["sub_error"],
		SubCheckedAt:   unixField(fields["sub_checked_at"]),
		BlockedAt:      unixField(fields["blocked_at"]),
		GrantState:     manage.GrantState(fields["grant_state"]),
		GrantCheckedAt: unixField(fields["grant_checked_at"]),
	}
}

func (r *Registry) Get(ctx context.Context, broadcasterID string) (manage.Channel, bool, error) {

	ch, err := r.cache.GetOrLoad(ctx, broadcasterID, func(ctx context.Context) (manage.Channel, error) {
		fields, err := pkg_valkey.HashAll(ctx, r.client, keyPrefix+broadcasterID)
		if err != nil {
			return manage.Channel{}, err
		}
		return channelFromFields(broadcasterID, fields), nil
	})

	if err != nil {
		return manage.Channel{}, false, err
	}

	if ch.BroadcasterID == "" {
		return manage.Channel{}, false, nil
	}

	return ch, true, nil
}

func (r *Registry) SetFlags(ctx context.Context, broadcasterID string, enabled, isMod *bool) error {
	key := keyPrefix + broadcasterID
	now := strconv.FormatInt(time.Now().Unix(), 10)
	fields := map[string]string{"updated_at": now}
	if enabled != nil {
		fields["enabled"] = utils.BoolField(*enabled)
	}
	if isMod != nil {
		fields["is_mod"] = utils.BoolField(*isMod)
		fields["mod_checked_at"] = now
	}
	update := pkg_valkey.HashFieldsUpdate{Key: key, Defaults: map[string]string{"enabled": "1"}, Fields: fields}
	if err := update.Apply(ctx, r.client, pkg_valkey.Membership{Key: indexKey, Member: broadcasterID, Include: true}); err != nil {
		return err
	}
	return r.afterChannelUpdate(ctx, broadcasterID)
}

func (r *Registry) SetMod(ctx context.Context, broadcasterID string, isMod bool) error {
	key := keyPrefix + broadcasterID
	now := strconv.FormatInt(time.Now().Unix(), 10)

	update := pkg_valkey.HashFieldsUpdate{
		Key:      key,
		Defaults: map[string]string{"enabled": "1"},
		Fields:   map[string]string{"is_mod": utils.BoolField(isMod), "mod_checked_at": now, "updated_at": now},
	}
	if err := update.Apply(ctx, r.client, pkg_valkey.Membership{Key: indexKey, Member: broadcasterID, Include: true}); err != nil {
		return err
	}
	return r.afterChannelUpdate(ctx, broadcasterID)
}

func (r *Registry) SetSubState(ctx context.Context, broadcasterID, state, errMsg string) error {
	key := keyPrefix + broadcasterID
	now := strconv.FormatInt(time.Now().Unix(), 10)

	update := pkg_valkey.HashFieldsUpdate{
		Key:    key,
		Fields: map[string]string{"sub_state": state, "sub_error": errMsg, "sub_checked_at": now, "updated_at": now},
	}
	err := update.Apply(ctx, r.client,
		pkg_valkey.Membership{Key: indexKey, Member: broadcasterID, Include: true},
		pkg_valkey.Membership{Key: unhealthyKey, Member: broadcasterID, Include: unhealthyState(state)},
	)
	if err != nil {
		return err
	}
	return r.afterChannelUpdate(ctx, broadcasterID)
}

func unhealthyState(state string) bool { return state == "failing" || state == "pending" }

func (r *Registry) UnhealthyBatch(ctx context.Context, n int) ([]string, error) {
	return pkg_valkey.ExpiringSet{Key: unhealthyKey}.Sample(ctx, r.client, n)
}

func (r *Registry) ClaimUnhealthySweep(ctx context.Context, ttl time.Duration) (bool, error) {
	return r.acquireLock(ctx, namedLock{key: unhealthySweepLockKey, owner: "1"}, ttl)
}

type SubState struct {
	State string
	Error string
}

func (r *Registry) SetSubStateUnless(ctx context.Context, broadcasterID string, write SubState, protected ...string) (bool, error) {
	now := strconv.FormatInt(time.Now().Unix(), 10)
	transition := pkg_valkey.GuardedTransition{
		Key: keyPrefix + broadcasterID, Index: indexKey, Flagged: unhealthyKey,
		GuardField: "sub_state",
		Fields: []pkg_valkey.FieldValue{
			{Field: "sub_state", Value: write.State},
			{Field: "sub_error", Value: write.Error},
			{Field: "sub_checked_at", Value: now},
			{Field: "updated_at", Value: now},
		},
		Member: broadcasterID, Flag: unhealthyState(write.State), Protected: protected,
	}
	applied, err := transition.Apply(ctx, r.client)
	if err != nil || !applied {
		return false, err
	}
	return true, r.afterChannelUpdate(ctx, broadcasterID)
}

func (r *Registry) MarkBlocked(ctx context.Context, ch manage.Channel) error {
	key := keyPrefix + ch.BroadcasterID
	now := strconv.FormatInt(time.Now().Unix(), 10)

	update := pkg_valkey.HashFieldsUpdate{Key: key, Fields: map[string]string{"blocked_at": now, "updated_at": now}}
	if err := update.Apply(ctx, r.client, pkg_valkey.Membership{Key: indexKey, Member: ch.BroadcasterID, Include: true}); err != nil {
		return err
	}
	return r.afterChannelUpdate(ctx, ch.BroadcasterID)
}

func (r *Registry) SetGrantState(ctx context.Context, broadcasterID string, state manage.GrantState) error {
	key := keyPrefix + broadcasterID
	now := strconv.FormatInt(time.Now().Unix(), 10)

	update := pkg_valkey.HashFieldsUpdate{
		Key:      key,
		Defaults: map[string]string{"enabled": "1"},
		Fields:   map[string]string{"grant_state": string(state), "grant_checked_at": now, "updated_at": now},
	}
	if err := update.Apply(ctx, r.client, pkg_valkey.Membership{Key: indexKey, Member: broadcasterID, Include: true}); err != nil {
		return err
	}
	return r.afterChannelUpdate(ctx, broadcasterID)
}

func (r *Registry) afterChannelUpdate(ctx context.Context, broadcasterID string) error {
	if err := r.retainHot(ctx, broadcasterID); err != nil {
		return err
	}
	r.cache.Invalidate(broadcasterID)
	r.publishInvalidation(broadcasterID)
	return nil
}

const enrollLockPrefix = "outgress:enroll:lock:"

const modCheckLockPrefix = "outgress:mod-check:lock:"

const verifyThrottlePrefix = "outgress:verify:"

func (r *Registry) ClaimVerify(ctx context.Context, broadcasterID string, ttl time.Duration) (bool, error) {
	return r.acquireLock(ctx, namedLock{key: verifyThrottlePrefix + broadcasterID, owner: "1"}, ttl)
}

const enrollCooldownPrefix = "outgress:enroll:cooldown:"

func (r *Registry) ArmEnrollCooldown(ctx context.Context, broadcasterID string, ttl time.Duration) error {
	return pkg_valkey.NewKV(r.client).SetMillis(ctx, enrollCooldownPrefix+broadcasterID, "1", ttl)
}

func (r *Registry) EnrollCooldownActive(ctx context.Context, broadcasterID string) (bool, error) {
	n, err := pkg_valkey.CountExisting(ctx, r.client, []string{enrollCooldownPrefix + broadcasterID})
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

const reauthBeaconPrefix = "outgress:reauth:beacon:"

// namedLock names a lock by its prefixed key and owner, so acquire and release share one shape.
type namedLock struct {
	key   string
	owner string
}

func (r *Registry) ArmReauthBeacon(ctx context.Context, broadcasterID string, ttl time.Duration) (bool, error) {
	return r.acquireLock(ctx, namedLock{key: reauthBeaconPrefix + broadcasterID, owner: "1"}, ttl)
}

func (r *Registry) AcquireEnrollLock(ctx context.Context, broadcasterID, owner string, ttl time.Duration) (bool, error) {
	return r.acquireLock(ctx, namedLock{key: enrollLockPrefix + broadcasterID, owner: owner}, ttl)
}

func (r *Registry) AcquireModCheckLock(ctx context.Context, broadcasterID, owner string, ttl time.Duration) (bool, error) {
	return r.acquireLock(ctx, namedLock{key: modCheckLockPrefix + broadcasterID, owner: owner}, ttl)
}

func (r *Registry) acquireLock(ctx context.Context, lock namedLock, ttl time.Duration) (bool, error) {
	return pkg_valkey.NewOwnerLock(r.client, lock.key, lock.owner).Acquire(ctx, ttl)
}

func (r *Registry) ReleaseEnrollLock(ctx context.Context, broadcasterID, owner string) error {
	return r.releaseLock(ctx, namedLock{key: enrollLockPrefix + broadcasterID, owner: owner})
}

func (r *Registry) ReleaseModCheckLock(ctx context.Context, broadcasterID, owner string) error {
	return r.releaseLock(ctx, namedLock{key: modCheckLockPrefix + broadcasterID, owner: owner})
}

func (r *Registry) releaseLock(ctx context.Context, lock namedLock) error {
	return pkg_valkey.NewOwnerLock(r.client, lock.key, lock.owner).Release(ctx)
}

func (r *Registry) List(ctx context.Context) ([]manage.Channel, error) {
	if err := r.PruneHot(ctx); err != nil {
		return nil, err
	}
	out := []manage.Channel{}
	for offset := 0; ; offset += hotBatch {
		ids, err := pkg_valkey.ZRangeByIndex(ctx, r.client, pkg_valkey.RankRange{Key: hotIndexKey, Start: int64(offset), Stop: int64(offset + hotBatch - 1)})
		if err != nil {
			return nil, err
		}
		if len(ids) == 0 {
			return out, nil
		}
		channels, err := r.listChannels(ctx, ids)
		if err != nil {
			return nil, err
		}
		out = append(out, channels...)

		if len(ids) < hotBatch {
			return out, nil
		}
	}
}

func (r *Registry) SetPaused(ctx context.Context, paused bool) error {
	if r.pauseStore != nil {
		return r.setDurablePause(ctx, paused)
	}
	version, err := (pkg_valkey.VersionedFlag{Flag: pausedKey, Version: pausedVersionKey}).Set(ctx, r.client, paused)
	if err != nil {
		return err
	}

	r.applyPauseSnapshot(pauseSnapshot{paused: paused, version: version, observedAt: time.Now()})
	r.publishPause(paused, version)
	return nil
}

func (r *Registry) SeedPause(paused bool) {
	r.applyPauseSnapshot(pauseSnapshot{paused: paused, observedAt: time.Now().Add(24 * time.Hour)})
}

func (r *Registry) Prime(ch manage.Channel) {
	r.cache.Set(ch.BroadcasterID, ch)
}

func (r *Registry) Paused(_ context.Context) (bool, error) {
	snapshot := r.pause.Load()
	if snapshot == nil || time.Since(snapshot.observedAt) > pauseMaxAge {
		return false, ErrPauseStateUnavailable
	}
	return snapshot.paused, nil
}

func (r *Registry) publishPause(paused bool, version int64) {
	if r.nc == nil || r.invalidatePrefix == "" {
		return
	}
	body, err := codec.Marshal(pauseEvent{Paused: paused, Version: version})
	if err == nil {
		err = r.nc.Publish(r.invalidatePrefix+"."+pauseInvalidateScope, body)
	}
	if err != nil && r.log != nil {
		r.log.Warn("failed to publish pause cache invalidation", zap.Error(err))
	}
}

func (r *Registry) loadPauseSnapshot(ctx context.Context) (pauseSnapshot, error) {
	if r.pauseStore != nil {
		return r.loadDurablePause(ctx)
	}
	paused, version, err := (pkg_valkey.VersionedFlag{Flag: pausedKey, Version: pausedVersionKey}).Read(ctx, r.client)
	if err != nil {
		return pauseSnapshot{}, err
	}
	return pauseSnapshot{paused: paused, version: version, observedAt: time.Now()}, nil
}

func (r *Registry) applyPauseSnapshot(next pauseSnapshot) {
	for {
		current := r.pause.Load()
		if current != nil && next.version < current.version {
			return
		}
		copy := next
		if r.pause.CompareAndSwap(current, &copy) {
			return
		}
	}
}

func (r *Registry) reconcilePause(ctx context.Context) {
	defer r.pauseWG.Done()
	timer := time.NewTimer(nextPauseReconcileDelay())
	defer timer.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
			readCtx, cancel := context.WithTimeout(ctx, pauseReadTimeout)
			snapshot, err := r.loadPauseSnapshot(readCtx)
			cancel()
			if err != nil {
				if r.log != nil {
					r.log.Warn("pause state reconciliation failed", zap.Error(err))
				}
			} else {
				r.applyPauseSnapshot(snapshot)
			}
			timer.Reset(nextPauseReconcileDelay())
		}
	}
}

func nextPauseReconcileDelay() time.Duration {
	return pauseReconcileInterval - pauseReconcileJitter/2 + rand.N(pauseReconcileJitter)
}

func unixField(v string) time.Time {
	sec, err := strconv.ParseInt(v, 10, 64)
	if err != nil || sec <= 0 {
		return time.Time{}
	}
	return time.Unix(sec, 0)
}

type channelIDs []string

func (r *Registry) listChannels(ctx context.Context, ids channelIDs) ([]manage.Channel, error) {
	keys := make([]string, len(ids))
	for i, id := range ids {
		keys[i] = keyPrefix + id
	}
	out := make([]manage.Channel, 0, len(ids))
	for i, read := range pkg_valkey.ReadHashes(ctx, r.client, keys) {
		if read.Err != nil {
			return nil, read.Err
		}
		ch := channelFromFields(ids[i], read.Fields)
		if ch.BroadcasterID != "" {
			out = append(out, ch)
		}
	}
	return out, nil
}
