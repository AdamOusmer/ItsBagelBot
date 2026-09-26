// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package worker

import (
	"context"
	"errors"
	"time"

	"ItsBagelBot/app/twitch/outgress/internal/twitch"
	"ItsBagelBot/internal/domain/outgress"
	"ItsBagelBot/pkg/ratelimit"
)

const (
	chatCapacity      = 20.0
	chatModCapacity   = 100.0
	chatWindowSeconds = 30.0

	helixCapacity        = 800.0
	helixWindowSeconds   = 60.0
	helixSystemReserve   = 100.0
	helixGeneralCapacity = helixCapacity - helixSystemReserve
	helixUserCapacity    = 800.0
)

const (
	helixAppKey         = "ratelimit:helix:app"
	helixAppStandardKey = "ratelimit:helix:app:standard"
)

const (
	systemGuardRetryMax        = 750 * time.Millisecond
	systemGuardActivationSlack = 25 * time.Millisecond
)

var (
	chatSpec              = ratelimit.NewSpec(chatCapacity, chatCapacity/chatWindowSeconds)
	chatModSpec           = ratelimit.NewSpec(chatModCapacity, chatModCapacity/chatWindowSeconds)
	helixStandardSpec     = ratelimit.NewSpec(helixGeneralCapacity/2, helixGeneralCapacity/helixWindowSeconds/2)
	helixSystemSpec       = ratelimit.NewSpec(helixSystemReserve, helixSystemReserve/helixWindowSeconds)
	helixUserSpec         = ratelimit.NewSpec(helixUserCapacity, helixUserCapacity/helixWindowSeconds)
	helixUserStandardSpec = ratelimit.NewSpec(helixUserCapacity/2, helixUserCapacity/helixWindowSeconds/2)
)

func generalHelixRequests(payload *outgress.Message) (standard, shared ratelimit.Request) {
	identity := twitch.ResolveIdentity(twitch.ParseIdentity(payload.As), payload.Endpoint)
	switch identity {
	case twitch.IdentityBot:
		shared = ratelimit.HelixBotRequest()
		standard = helixUserStandardSpec.ForKey("ratelimit:helix:user:bot:standard")
	case twitch.IdentityBroadcaster:
		shared = helixUserSpec.ForDynamicKey("ratelimit:helix:user:", "helix:user", payload.BroadcasterID)
		standard = helixUserStandardSpec.ForDynamicKey("ratelimit:helix:user:standard:", "helix:user:standard", payload.BroadcasterID)
	default:
		shared = ratelimit.HelixAppRequest()
		standard = helixStandardSpec.ForKey(helixAppStandardKey)
	}
	return standard, shared
}

func (w *Worker) takeChat(ctx context.Context, broadcasterID string, isMod bool) error {
	spec := chatSpec
	if isMod {
		spec = chatModSpec
	}
	return w.take(ctx, spec.ForDynamicKey("ratelimit:chat:", "chat", broadcasterID))
}

func (w *Worker) takeGeneralHelix(ctx context.Context, payload *outgress.Message) error {
	standard, shared := generalHelixRequests(payload)
	if w.lane == LaneStandard {
		return w.takeOrdered(ctx, standard, shared)
	}
	return w.take(ctx, shared)
}

func (w *Worker) takeAppHelix(ctx context.Context) error {
	shared := ratelimit.HelixAppRequest()
	if w.lane == LaneStandard {
		return w.takeOrdered(ctx, helixStandardSpec.ForKey(helixAppStandardKey), shared)
	}
	return w.take(ctx, shared)
}

func (w *Worker) takeSystemHelix(ctx context.Context) error {
	err := w.takeSystem(ctx, helixSystemSpec.ForKey("ratelimit:helix:system"))
	if !errors.Is(err, errRateLimitShared) {
		return err
	}
	return w.takeSystem(ctx, ratelimit.HelixAppRequest())
}

func (w *Worker) takeSystem(ctx context.Context, req ratelimit.Request) error {
	err := w.take(ctx, req)
	if !errors.Is(err, errRateLimitShared) {
		return err
	}
	return w.retrySystemAfterGuard(ctx, req)
}

func (w *Worker) retrySystemAfterGuard(ctx context.Context, req ratelimit.Request) error {
	wait, ok := systemGuardRetryDelay(w.limiter)
	if !ok {
		return errRateLimitShared
	}
	if err := waitForSystemGuard(ctx, wait); err != nil {
		return err
	}
	return w.take(ctx, req)
}

func systemGuardRetryDelay(manager ratelimit.Manager) (time.Duration, bool) {
	wait := ratelimit.GuardRetryAfter(manager)
	if wait <= 0 || wait > systemGuardRetryMax {
		return 0, false
	}
	wait += systemGuardActivationSlack
	if wait > systemGuardRetryMax {
		wait = systemGuardRetryMax
	}
	return wait, true
}

func waitForSystemGuard(ctx context.Context, wait time.Duration) error {
	timer := time.NewTimer(wait)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func (w *Worker) take(ctx context.Context, req ratelimit.Request) error {
	started := time.Now()
	defer recordStageDuration(ctx, "outgress.limiter_ms", started)
	allowed, err := w.limiter.Allow(ctx, req)
	if err != nil {
		return err
	}
	if !allowed {
		return errRateLimitShared
	}
	return nil
}

func (w *Worker) takeOrdered(ctx context.Context, first, shared ratelimit.Request) error {
	started := time.Now()
	defer recordStageDuration(ctx, "outgress.limiter_ms", started)
	denied, err := w.limiter.AllowOrdered(ctx, first, shared)
	if err != nil {
		return err
	}
	switch denied {
	case 0:
		return nil
	case 1:
		return errRateLimitFirst
	default:
		return errRateLimitShared
	}
}
