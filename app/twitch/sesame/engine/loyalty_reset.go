// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"context"
	"strconv"
	"strings"
	"time"

	"ItsBagelBot/app/twitch/sesame/module"
	"ItsBagelBot/internal/domain/invalidate"
	"ItsBagelBot/internal/projection"
	"ItsBagelBot/pkg/cache"
	"ItsBagelBot/pkg/codec"
	pkg_valkey "ItsBagelBot/pkg/valkey"

	"github.com/nats-io/nats.go"
	"github.com/valkey-io/valkey-go"
	"go.uber.org/zap"
)

const (
	loyaltyResetKeyPrefix     = "loyalty:reset:"
	loyaltyResetDonePrefix    = loyaltyResetKeyPrefix + "done:"
	loyaltyResetClaimPrefix   = loyaltyResetKeyPrefix + "claim:"
	loyaltyResetDueKey        = loyaltyResetKeyPrefix + "due"
	loyaltyResetSweepInterval = time.Minute
	loyaltyResetSweepBatch    = 100
	loyaltyResetClaimTTL      = 30 * time.Second
	loyaltyResetOpTimeout     = 45 * time.Second
	loyaltyResetRearmTimeout  = 5 * time.Second
)

type LoyaltyResetter interface {
	BalanceReset(ctx context.Context, broadcasterID uint64) error
}

type LoyaltyResetConfig struct {
	NC                       *nats.Conn
	ModulesInvalidateSubject string
	KeyspaceDB               int
	Expiry                   *pkg_valkey.ExpiryHub
	Log                      *zap.Logger
}

type LoyaltyResetClock struct {
	client   valkey.Client
	proj     projection.Reader
	resetter LoyaltyResetter
	cfg      LoyaltyResetConfig
	log      *zap.Logger
	now      func() time.Time
}

func NewLoyaltyResetClock(client valkey.Client, proj projection.Reader, resetter LoyaltyResetter, cfg LoyaltyResetConfig) *LoyaltyResetClock {
	log := cfg.Log
	if log == nil {
		log = zap.NewNop()
	}
	return &LoyaltyResetClock{client: pkg_valkey.Primary(client), proj: proj, resetter: resetter, cfg: cfg, log: log, now: time.Now}
}

func (c *LoyaltyResetClock) schedule(ctx context.Context, id uint64) (ResetSchedule, ModuleState) {
	view, state, _ := ModuleLookup{Proj: c.proj, BroadcasterID: id, Name: LoyaltyModuleName, Absent: ModuleOff}.Resolve(ctx)
	if state != ModuleOn {
		return ResetOff, state
	}
	var cfg LoyaltyModuleConfig
	if len(view.Configs) > 0 && codec.Unmarshal(view.Configs, &cfg) != nil {
		return ResetOff, state
	}
	return ResetSchedule(cfg.ResetSchedule), state
}

// Converge arms, re-arms or disarms the clock after a config change; a projection outage leaves it untouched.
func (c *LoyaltyResetClock) Converge(ctx context.Context, id uint64) {
	schedule, state := c.schedule(ctx, id)
	switch {
	case state == ModuleUnavailable:
		return
	case schedule.Enabled():
		c.logErr("arm", id, c.arm(ctx, id, schedule))
	default:
		c.logErr("disarm", id, c.disarm(ctx, id))
	}
}

func (c *LoyaltyResetClock) arm(ctx context.Context, id uint64, schedule ResetSchedule) error {
	next := NextResetBoundary(schedule, c.now(), time.UTC)
	wait := max(next.Sub(c.now()), time.Millisecond)
	results := c.client.DoMulti(ctx,
		c.client.B().Set().Key(cache.UserKey(loyaltyResetKeyPrefix, id)).Value("1").PxMilliseconds(wait.Milliseconds()).Build(),
		c.client.B().Zadd().Key(loyaltyResetDueKey).ScoreMember().ScoreMember(float64(next.UnixMilli()), strconv.FormatUint(id, 10)).Build(),
	)
	for _, r := range results {
		if err := r.Error(); err != nil {
			return err
		}
	}
	return nil
}

func (c *LoyaltyResetClock) disarm(ctx context.Context, id uint64) error {
	results := c.client.DoMulti(ctx,
		c.client.B().Del().Key(cache.UserKey(loyaltyResetKeyPrefix, id)).Build(),
		c.client.B().Zrem().Key(loyaltyResetDueKey).Member(strconv.FormatUint(id, 10)).Build(),
	)
	for _, r := range results {
		if err := r.Error(); err != nil {
			return err
		}
	}
	return nil
}

func (c *LoyaltyResetClock) logErr(action string, id uint64, err error) {
	if err != nil {
		c.log.Warn("loyalty reset: "+action+" failed", module.BIDField(id), zap.Error(err))
	}
}

// Fire resets the channel once per period, then arms the next boundary.
func (c *LoyaltyResetClock) Fire(ctx context.Context, id uint64) {
	won, err := pkg_valkey.ClaimOnce(ctx, c.client, cache.UserKey(loyaltyResetClaimPrefix, id), loyaltyResetClaimTTL)
	if err != nil || !won {
		return
	}
	schedule, state := c.schedule(ctx, id)
	switch {
	case state == ModuleUnavailable:
		return
	case !schedule.Enabled():
		c.logErr("disarm", id, c.disarm(ctx, id))
	default:
		c.settle(ctx, id, schedule)
	}
}

func (c *LoyaltyResetClock) settle(ctx context.Context, id uint64, schedule ResetSchedule) {
	if err := c.resetOnce(ctx, id, schedule); err != nil {
		c.logErr("reset", id, err)
		return
	}
	c.logErr("arm", id, c.arm(ctx, id, schedule))
}

func (c *LoyaltyResetClock) resetOnce(ctx context.Context, id uint64, schedule ResetSchedule) error {
	start := ResetPeriodStart(schedule, c.now(), time.UTC)
	doneKey := loyaltyResetDonePrefix + strconv.FormatUint(id, 10) + ":" + strconv.FormatInt(start.Unix(), 10)
	fresh, err := pkg_valkey.ClaimOnce(ctx, c.client, doneKey, schedule.doneRetention())
	if err != nil || !fresh {
		return err
	}
	if err := c.resetter.BalanceReset(ctx, id); err != nil {
		_ = c.client.Do(ctx, c.client.B().Del().Key(doneKey).Build()).Error()
		return err
	}
	return nil
}

func loyaltyResetKeyID(key string) (uint64, bool) {
	rest, ok := strings.CutPrefix(key, loyaltyResetKeyPrefix)
	if !ok {
		return 0, false
	}
	id, err := strconv.ParseUint(rest, 10, 64)
	return id, err == nil && id != 0
}

func (c *LoyaltyResetClock) fireBounded(ctx context.Context, id uint64) {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), loyaltyResetOpTimeout)
	defer cancel()
	c.Fire(ctx, id)
}

func (c *LoyaltyResetClock) onExpired(ctx context.Context, key string) {
	if id, ok := loyaltyResetKeyID(key); ok {
		go c.fireBounded(ctx, id)
	}
}

// Expiry events are lost while no replica is subscribed; the due sweep still fires those resets.
func (c *LoyaltyResetClock) StartExpiryWatcher(ctx context.Context) {
	go c.sweepLoop(ctx)
	pkg_valkey.ExpiryHubOr(c.cfg.Expiry, c.client, c.cfg.KeyspaceDB, c.log).Watch(ctx, c.onExpired, loyaltyResetKeyPrefix)
}

func (c *LoyaltyResetClock) sweepLoop(ctx context.Context) {
	ticker := time.NewTicker(loyaltyResetSweepInterval)
	defer ticker.Stop()
	for {
		c.Sweep(ctx)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (c *LoyaltyResetClock) overdue(ctx context.Context) []uint64 {
	raw, err := c.client.Do(ctx, c.client.B().Zrangebyscore().Key(loyaltyResetDueKey).
		Min("-inf").Max(strconv.FormatInt(c.now().UnixMilli(), 10)).Limit(0, loyaltyResetSweepBatch).Build()).AsStrSlice()
	if err != nil && ctx.Err() == nil {
		c.log.Warn("loyalty reset: overdue sweep failed", zap.Error(err))
	}
	ids := make([]uint64, 0, len(raw))
	for _, member := range raw {
		if id, parseErr := strconv.ParseUint(member, 10, 64); parseErr == nil && id != 0 {
			ids = append(ids, id)
		}
	}
	return ids
}

func (c *LoyaltyResetClock) Sweep(ctx context.Context) {
	for _, id := range c.overdue(ctx) {
		go c.fireBounded(ctx, id)
	}
}

func (c *LoyaltyResetClock) StartRearmWatcher(ctx context.Context) {
	if c.cfg.NC == nil || c.cfg.ModulesInvalidateSubject == "" {
		return
	}
	sub, err := c.cfg.NC.Subscribe(c.cfg.ModulesInvalidateSubject, func(msg *nats.Msg) {
		var dto invalidate.DTO
		if codec.Unmarshal(msg.Data, &dto) != nil {
			return
		}
		if id, err := strconv.ParseUint(dto.BroadcasterID, 10, 64); err == nil && id != 0 {
			go c.convergeBounded(id)
		}
	})
	if err != nil {
		c.log.Error("loyalty reset: rearm watcher unavailable", zap.Error(err))
		return
	}
	defer sub.Unsubscribe()
	<-ctx.Done()
}

func (c *LoyaltyResetClock) convergeBounded(id uint64) {
	ctx, cancel := context.WithTimeout(context.Background(), loyaltyResetRearmTimeout)
	defer cancel()
	c.Converge(ctx, id)
}
