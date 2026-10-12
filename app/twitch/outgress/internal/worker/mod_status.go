// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package worker

import (
	"context"
	"sync"
	"time"

	"ItsBagelBot/app/twitch/outgress/internal/channels"
	"ItsBagelBot/app/twitch/outgress/internal/twitch"
	"ItsBagelBot/internal/domain/rpc/manage"

	"go.uber.org/zap"
)

const (
	modStatusTTL         = 24 * time.Hour
	modCheckTimeout      = 15 * time.Second
	modCheckErrorBackoff = 5 * time.Minute
	maxConcurrentChecks  = 4
	backoffPruneAbove    = 1024
)

type ModVerifier struct {
	Admit    func(context.Context, string) bool
	registry modRegistry
	twitch   moderatorClient
	botID    string
	owner    string
	log      *zap.Logger

	mu      sync.Mutex
	pending map[string]struct{}
	backoff map[string]time.Time
	slots   chan struct{}
	wg      sync.WaitGroup
	closed  bool
}

type modRegistry interface {
	AcquireModCheckLock(context.Context, string, string, time.Duration) (bool, error)
	ReleaseModCheckLock(context.Context, string, string) error
	SetMod(context.Context, string, bool) error
}

type moderatorClient interface {
	HasUserToken() bool
	IsModerator(context.Context, string, string) (bool, error)
}

func NewModVerifier(registry *channels.Registry, tw *twitch.Client, botID, owner string, log *zap.Logger) *ModVerifier {
	return newModVerifier(registry, tw, botID, owner, log)
}

func newModVerifier(registry modRegistry, tw moderatorClient, botID, owner string, log *zap.Logger) *ModVerifier {
	return &ModVerifier{
		registry: registry,
		twitch:   tw,
		botID:    botID,
		owner:    owner,
		log:      log,
		pending:  make(map[string]struct{}),
		backoff:  make(map[string]time.Time),
		slots:    make(chan struct{}, maxConcurrentChecks),
	}
}

func (v *ModVerifier) Status(ch manage.Channel, found bool, broadcasterID, senderID string) bool {
	if found && !ch.ModCheckedAt.IsZero() && time.Since(ch.ModCheckedAt) < modStatusTTL {
		return ch.IsMod
	}
	v.Schedule(broadcasterID, senderID)
	return found && ch.IsMod
}

type moderatorCheck struct{ broadcasterID, botID string }

func (v *ModVerifier) Schedule(broadcasterID, senderID string) {
	check := moderatorCheck{broadcasterID: broadcasterID}
	if !v.canSchedule(check) {
		return
	}
	botID := v.botID
	if botID == "" {
		botID = senderID
	}
	if botID == "" {
		return
	}

	if !v.reserveCheck(check) {
		return
	}

	check.botID = botID
	go v.verify(check)
}

func (v *ModVerifier) Close() {
	v.mu.Lock()
	v.closed = true
	v.mu.Unlock()
	v.wg.Wait()
}

func (v *ModVerifier) verify(check moderatorCheck) {
	failed := false
	defer func() { v.finishCheck(check, failed) }()

	ctx, cancel := context.WithTimeout(context.Background(), modCheckTimeout)
	defer cancel()

	if !v.admitted(ctx, check) {
		return
	}
	got, err := v.registry.AcquireModCheckLock(ctx, check.broadcasterID, v.owner, modCheckErrorBackoff)
	if err != nil {
		failed = true
		v.log.Warn("mod status lock failed", zap.String("broadcaster_id", check.broadcasterID), zap.Error(err))
		return
	}
	if !got {
		failed = true
		return
	}

	if !v.admitted(ctx, check) {
		return
	}
	isMod, err := v.twitch.IsModerator(ctx, check.botID, check.broadcasterID)
	if err != nil {
		failed = true
		v.log.Warn("mod status verification failed; preserving last known state",
			zap.String("broadcaster_id", check.broadcasterID), zap.Error(err))
		return
	}
	if err := v.registry.SetMod(ctx, check.broadcasterID, isMod); err != nil {
		failed = true
		v.log.Warn("failed to persist mod status", zap.String("broadcaster_id", check.broadcasterID), zap.Error(err))
		return
	}

	releaseCtx, releaseCancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer releaseCancel()
	if err := v.registry.ReleaseModCheckLock(releaseCtx, check.broadcasterID, v.owner); err != nil {
		v.log.Warn("failed to release mod status lock", zap.String("broadcaster_id", check.broadcasterID), zap.Error(err))
	}
	v.log.Debug("mod status refreshed",
		zap.String("broadcaster_id", check.broadcasterID), zap.Bool("is_mod", isMod))
}

func (v *ModVerifier) canSchedule(check moderatorCheck) bool {
	if v == nil {
		return false
	}
	if !v.admitted(context.Background(), check) {
		return false
	}
	if check.broadcasterID == "" {
		return false
	}
	return v.twitch.HasUserToken()
}
func (v *ModVerifier) admitted(ctx context.Context, check moderatorCheck) bool {
	if v.Admit == nil {
		return true
	}
	return v.Admit(ctx, check.broadcasterID)
}
func (v *ModVerifier) reserveCheck(check moderatorCheck) bool {
	now := time.Now()
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.closed {
		return false
	}
	if _, ok := v.pending[check.broadcasterID]; ok {
		return false
	}
	if now.Before(v.backoff[check.broadcasterID]) {
		return false
	}
	select {
	case v.slots <- struct{}{}:
		v.pending[check.broadcasterID] = struct{}{}
	default:
		return false
	}
	v.wg.Add(1)
	return true
}

func (v *ModVerifier) finishCheck(check moderatorCheck, failed bool) {
	defer v.wg.Done()
	<-v.slots
	v.mu.Lock()
	defer v.mu.Unlock()
	delete(v.pending, check.broadcasterID)
	now := time.Now()
	if failed {
		v.backoff[check.broadcasterID] = now.Add(modCheckErrorBackoff)
	} else {
		delete(v.backoff, check.broadcasterID)
	}
	v.pruneBackoff(now)
}

// Called while the verifier mutex is held.
func (v *ModVerifier) pruneBackoff(now time.Time) {
	if len(v.backoff) <= backoffPruneAbove {
		return
	}
	for id, until := range v.backoff {
		if now.After(until) {
			delete(v.backoff, id)
		}
	}
}
