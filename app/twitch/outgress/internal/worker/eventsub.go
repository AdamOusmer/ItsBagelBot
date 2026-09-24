// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package worker

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"ItsBagelBot/app/twitch/outgress/internal/twitch"
	"ItsBagelBot/internal/domain/outgress"
	"ItsBagelBot/internal/domain/rpc/manage"
	"ItsBagelBot/pkg/codec"

	"go.uber.org/zap"
)

const enrollCooldownTTL = 2 * time.Minute

const (
	enrollLockTTL          = 60 * time.Second
	enrollLockWaitTimeout  = 3 * time.Second
	enrollLockPollInterval = 100 * time.Millisecond
)

// Persisted as sub_state; the console matches these exact strings.
const (
	subStateOK      = "ok"
	subStatePending = "pending"
	subStateFailing = "failing"
	subStateRevoked = "revoked"
	subStateBanned  = "chat_banned"
)

type enrollment struct {
	broadcasterID string
	conduitID     string
}

func (w *Worker) processEventSub(ctx context.Context, payload *outgress.Message) error {
	if payload.BroadcasterID == "" {
		w.log.Error("dropping eventsub job without broadcaster id")
		return nil
	}

	conduitID, err := w.conduit.Get(ctx)
	if err != nil {
		w.log.Warn("eventsub job cannot resolve conduit id, will retry",
			zap.String("broadcaster_id", payload.BroadcasterID),
			zap.Error(err))
		return err
	}

	var job outgress.EventSubJob
	if err := codec.Unmarshal(payload.Payload, &job); err != nil {
		w.log.Error("dropping malformed eventsub job", zap.Error(err))
		noticeError(ctx, err)
		return nil
	}

	e := enrollment{broadcasterID: payload.BroadcasterID, conduitID: conduitID}
	switch effectiveMode(job) {
	case outgress.ModeEnable:
		return w.enableEventSubs(ctx, e)
	case outgress.ModeDisable:
		return w.disableChannel(ctx, e)
	case outgress.ModeReconnect:
		return w.reconnectEventSubs(ctx, e)
	case outgress.ModeEnsureOptional:
		return w.ensureOptionalEventSubs(ctx, e)
	default:
		w.log.Error("dropping eventsub job with unknown mode",
			zap.String("mode", job.Mode),
			zap.String("broadcaster_id", payload.BroadcasterID))
		return nil
	}
}

func effectiveMode(job outgress.EventSubJob) string {
	if job.Mode != "" {
		return job.Mode
	}
	if job.Enabled {
		return outgress.ModeEnable
	}
	return outgress.ModeDisable
}

const errEnrollLockBusy expectedNackError = "enroll lock busy: another operation in progress for this channel"

func (w *Worker) underEnrollLock(ctx context.Context, op string, e enrollment, fn func() error) error {
	got, err := w.registry.AcquireEnrollLock(ctx, e.broadcasterID, w.owner, enrollLockTTL)
	if err != nil {
		return err
	}
	if !got {
		w.log.Info(op+" waiting on enroll lock",
			zap.String("broadcaster_id", e.broadcasterID))
		got, err = waitForEnrollLock(ctx, enrollLockWaitTimeout, enrollLockPollInterval, func() (bool, error) {
			return w.registry.AcquireEnrollLock(ctx, e.broadcasterID, w.owner, enrollLockTTL)
		})
		if err != nil {
			return err
		}
		if !got {
			return errEnrollLockBusy
		}
		w.log.Info(op+" acquired enroll lock after waiting",
			zap.String("broadcaster_id", e.broadcasterID))
	}
	defer func() { _ = w.registry.ReleaseEnrollLock(ctx, e.broadcasterID, w.owner) }()

	return fn()
}

func waitForEnrollLock(
	ctx context.Context,
	timeout time.Duration,
	pollInterval time.Duration,
	acquire func() (bool, error),
) (bool, error) {
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return false, ctx.Err()
		case <-timer.C:
			return false, nil
		case <-ticker.C:
			got, err := acquire()
			if err != nil || got {
				return got, err
			}
		}
	}
}

func (w *Worker) skipRevokedEnroll(ctx context.Context, e enrollment, op string) bool {
	ch, found, err := w.registry.Get(ctx, e.broadcasterID)
	if err != nil || !found {
		return false
	}
	if ch.SubState != subStateRevoked {
		return false
	}
	w.log.Info(op+" skipped: authorization revoked, waiting for the broadcaster to reconnect",
		zap.String("broadcaster_id", e.broadcasterID))
	return true
}

func (w *Worker) skipFreshEnroll(ctx context.Context, e enrollment, op string) bool {
	active, err := w.registry.EnrollCooldownActive(ctx, e.broadcasterID)
	if err != nil || !active {
		return false
	}
	ch, found, err := w.registry.Get(ctx, e.broadcasterID)
	if err != nil || !redundantEnroll(ch, found) {
		return false
	}
	w.log.Info(op+" skipped: subscriptions freshly enrolled and healthy",
		zap.String("broadcaster_id", e.broadcasterID))
	return true
}

func redundantEnroll(ch manage.Channel, found bool) bool {
	return found && ch.SubState == subStateOK
}

func (w *Worker) recordEnrollSuccess(ctx context.Context, e enrollment) {
	_ = w.registry.SetSubState(ctx, e.broadcasterID, subStateOK, "")
	_ = w.registry.ArmEnrollCooldown(ctx, e.broadcasterID, enrollCooldownTTL)
}

const (
	transientRetryAttempts = 3
	transientRetryStep     = 500 * time.Millisecond
)

func retryTransient(ctx context.Context, fn func() error) error {
	var lastErr error
	for attempt := 1; attempt <= transientRetryAttempts; attempt++ {
		lastErr = fn()
		if lastErr == nil || isPermanent(lastErr) {
			return lastErr
		}
		select {
		case <-ctx.Done():
			return lastErr
		case <-time.After(time.Duration(attempt) * transientRetryStep):
		}
	}
	return lastErr
}

func (w *Worker) enableEventSubs(ctx context.Context, e enrollment) error {
	return w.underEnrollLock(ctx, "enable", e, func() error {
		if w.skipFreshEnroll(ctx, e, "enable") || w.skipRevokedEnroll(ctx, e, "enable") {
			return nil
		}
		_ = w.registry.SetSubState(ctx, e.broadcasterID, subStatePending, "")

		err := retryTransient(ctx, func() error {
			return w.createAllEventSubs(ctx, e)
		})
		if err == nil {
			w.recordEnrollSuccess(ctx, e)
			w.log.Info("eventsub subscriptions created", zap.String("broadcaster_id", e.broadcasterID))
			w.seedLiveStatus(ctx, e.broadcasterID)
			return nil
		}

		w.recordEnrollFailure(ctx, e, "enable", err)
		return nil
	})
}

func (w *Worker) recordEnrollFailure(ctx context.Context, e enrollment, op string, err error) {
	switch {
	case isChatBanned(err):
		_ = w.blockChannel(ctx, e.broadcasterID, blockBanned.because(op+": "+err.Error()))
	case isAuthRevoked(err):
		_ = w.blockChannel(ctx, e.broadcasterID, blockRevoked.because(op+": "+err.Error()))
	default:
		_ = w.registry.SetSubState(ctx, e.broadcasterID, subStateFailing, err.Error())
		w.log.Error(op+": eventsubs not fully accepted, marked failing",
			zap.String("broadcaster_id", e.broadcasterID),
			zap.Error(err))
	}
}

func (w *Worker) disableChannel(ctx context.Context, e enrollment) error {
	return w.underEnrollLock(ctx, "disable", e, func() error {
		err := retryTransient(ctx, func() error {
			return w.disableEventSubs(ctx, e)
		})
		if err == nil {
			_ = w.registry.SetSubState(ctx, e.broadcasterID, "", "")
			return nil
		}

		_ = w.registry.SetSubState(ctx, e.broadcasterID, subStateFailing, err.Error())
		w.log.Error("disable: eventsubs not fully removed, marked failing",
			zap.String("broadcaster_id", e.broadcasterID),
			zap.Error(err))
		return nil
	})
}

func (w *Worker) reconnectEventSubs(ctx context.Context, e enrollment) error {
	return w.underEnrollLock(ctx, "reconnect", e, func() error {
		if w.skipFreshEnroll(ctx, e, "reconnect") || w.skipRevokedEnroll(ctx, e, "reconnect") {
			return nil
		}
		_ = w.registry.SetSubState(ctx, e.broadcasterID, subStatePending, "")

		if derr := w.disableEventSubs(ctx, e); derr != nil {
			w.log.Warn("reconnect: drop phase failed, proceeding to recreate",
				zap.String("broadcaster_id", e.broadcasterID),
				zap.Error(derr))
		}

		err := retryTransient(ctx, func() error {
			return w.createAllEventSubs(ctx, e)
		})
		if err == nil {
			w.recordEnrollSuccess(ctx, e)
			w.log.Info("reconnect: all eventsubs accepted",
				zap.String("broadcaster_id", e.broadcasterID))
			w.seedLiveStatus(ctx, e.broadcasterID)
			return nil
		}

		if isAuthRevoked(err) {
			w.recordEnrollFailure(ctx, e, "reconnect", err)
			return nil
		}

		_ = w.registry.SetSubState(ctx, e.broadcasterID, subStateFailing, err.Error())
		w.log.Error("reconnect: eventsubs not fully accepted, retrying",
			zap.String("broadcaster_id", e.broadcasterID),
			zap.Error(err))
		return err
	})
}

func (w *Worker) disableEventSubs(ctx context.Context, e enrollment) error {
	ids, err := w.ownedSubIDs(ctx, e)
	if err != nil {
		return w.eventSubFailure(ctx, err, "eventsub list", e.broadcasterID)
	}

	for _, id := range ids {
		if err := w.takeSystemHelix(ctx); err != nil {
			return err
		}
		if err := w.twitch.DeleteEventSub(ctx, id); err != nil {
			return w.eventSubFailure(ctx, err, "eventsub delete", e.broadcasterID)
		}
	}

	w.log.Info("eventsub subscriptions removed",
		zap.String("broadcaster_id", e.broadcasterID), zap.Int("deleted", len(ids)))
	return nil
}

func (w *Worker) ownedSubIDs(ctx context.Context, e enrollment) ([]string, error) {
	var ids []string
	cursor := ""
	for {
		if err := w.takeSystemHelix(ctx); err != nil {
			return nil, err
		}
		subs, next, err := w.twitch.ListEventSubs(ctx, e.broadcasterID, cursor)
		if err != nil {
			return nil, err
		}
		ids = appendOwnedSubIDs(ids, subs, e)
		if next == "" {
			return ids, nil
		}
		cursor = next
	}
}

func appendOwnedSubIDs(ids []string, subs []twitch.EventSubEntry, e enrollment) []string {
	for _, sub := range subs {
		if ownedSub(sub, e) {
			ids = append(ids, sub.ID)
		}
	}
	return ids
}

// Listing by the bot's id also returns every other channel's chat subscription.
func ownedSub(sub twitch.EventSubEntry, e enrollment) bool {
	if sub.Transport.ConduitID != e.conduitID {
		return false
	}
	return sub.Condition.BroadcasterUserID == "" || sub.Condition.BroadcasterUserID == e.broadcasterID
}

func (w *Worker) createAllEventSubs(ctx context.Context, e enrollment) error {
	if w.botID == "" {
		return fmt.Errorf("bot user id not configured: channel.chat.message cannot be created")
	}

	for _, spec := range twitch.ChannelSubscriptions(e.broadcasterID, w.botID) {
		if err := w.takeSystemHelix(ctx); err != nil {
			return err
		}
		if err := w.twitch.CreateEventSub(ctx, spec, e.conduitID); err != nil {
			w.conduit.Invalidate()
			return &createError{subType: spec.Type, err: err}
		}
	}

	return w.createOptionalEventSubs(ctx, e)
}

func (w *Worker) ensureOptionalEventSubs(ctx context.Context, e enrollment) error {
	return w.underEnrollLock(ctx, "ensure-optional", e, func() error {
		if err := w.createOptionalEventSubs(ctx, e); err != nil {
			w.log.Warn("ensure-optional eventsubs failed, will retry",
				zap.String("broadcaster_id", e.broadcasterID), zap.Error(err))
			return err
		}
		w.log.Info("optional eventsubs ensured", zap.String("broadcaster_id", e.broadcasterID))
		return nil
	})
}

func (w *Worker) createOptionalEventSubs(ctx context.Context, e enrollment) error {
	for _, spec := range twitch.ChannelOptionalSubscriptions(e.broadcasterID) {
		if err := w.takeSystemHelix(ctx); err != nil {
			return err
		}
		if err := w.twitch.CreateEventSub(ctx, spec, e.conduitID); err != nil {
			if isPermanent(err) {
				w.log.Info("optional eventsub not available for channel, skipping",
					zap.String("broadcaster_id", e.broadcasterID),
					zap.String("subscription", spec.Type),
					zap.Error(err))
				continue
			}
			w.conduit.Invalidate()
			return fmt.Errorf("create optional %s: %w", spec.Type, err)
		}
	}
	return nil
}

func (w *Worker) eventSubFailure(ctx context.Context, err error, op, broadcasterID string) error {
	if isPermanent(err) {
		w.log.Error("dropping eventsub job twitch rejected",
			zap.String("op", op),
			zap.String("broadcaster_id", broadcasterID),
			zap.Error(err))
		noticeError(ctx, err)
		return nil
	}

	w.log.Warn("eventsub job failed, will retry",
		zap.String("op", op),
		zap.String("broadcaster_id", broadcasterID),
		zap.Error(err))
	return err
}

func isPermanent(err error) bool {
	var se *twitch.StatusError
	if errors.As(err, &se) {
		return se.Status >= 400 && se.Status < 500 &&
			se.Status != http.StatusTooManyRequests &&
			se.Status != http.StatusUnauthorized
	}
	return false
}

func isAuthRevoked(err error) bool {
	var se *twitch.StatusError
	if !errors.As(err, &se) || se.Status != http.StatusForbidden {
		return false
	}
	return strings.Contains(se.Body, "subscription missing proper authorization")
}

type createError struct {
	subType string
	err     error
}

func (e *createError) Error() string { return "create " + e.subType + ": " + e.err.Error() }
func (e *createError) Unwrap() error { return e.err }

func isChatBanned(err error) bool {
	var ce *createError
	if !errors.As(err, &ce) || ce.subType != twitch.ChatMessageType {
		return false
	}
	return isAuthRevoked(err)
}
