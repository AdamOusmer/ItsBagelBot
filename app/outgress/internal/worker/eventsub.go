package worker

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"ItsBagelBot/app/outgress/internal/twitch"
	"ItsBagelBot/internal/domain/outgress"
	"ItsBagelBot/internal/domain/rpc/manage"

	"github.com/bytedance/sonic"

	"go.uber.org/zap"
)

// enrollCooldownTTL bounds how often a channel's full enroll may re-run while
// its subscriptions are verified healthy. A dashboard user spamming restart
// (or double-submitting enable) otherwise costs ~13-24 reserved Helix calls
// per click; within this window the redundant job is acknowledged without
// touching Twitch. Repair paths are unaffected: the skip requires sub_state
// "ok", so a failing, pending, or cleared channel always gets its enroll.
const enrollCooldownTTL = 2 * time.Minute

// enrollment identifies one channel's EventSub enrollment on our conduit: the
// broadcaster whose subscriptions are managed and the conduit they route to.
type enrollment struct {
	broadcasterID string
	conduitID     string
}

// processEventSub applies the receive toggle for one channel, paying the
// reserved system Helix bucket once per HTTP call. Conduit EventSub
// management runs under the app token. Chat (channel.chat.message) is read in
// the bot account's user context (the bot's user:read:chat / user:bot grant
// plus the broadcaster's channel:bot grant); the broadcaster events (subs,
// cheers, follows, channel.update title changes) are authorized by the
// broadcaster's own consent (channel:read:subscriptions, bits:read,
// moderator:read:followers). No bot user token is involved here. Creates are
// 409-idempotent and deletes 404-idempotent, so a job nacked halfway (rate
// limit, transient Twitch error) converges when redelivery re-runs it.
func (w *Worker) processEventSub(ctx context.Context, payload outgress.Message) error {
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
	if err := sonic.Unmarshal(payload.Payload, &job); err != nil {
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

// effectiveMode resolves a job's mode: an explicit Mode wins; empty falls back
// to the legacy Enabled field.
func effectiveMode(job outgress.EventSubJob) string {
	if job.Mode != "" {
		return job.Mode
	}
	if job.Enabled {
		return outgress.ModeEnable
	}
	return outgress.ModeDisable
}

// errEnrollLockBusy naks a job that lost the enroll-lock race so paced
// redelivery re-applies it after the holder finishes.
var errEnrollLockBusy = errors.New("enroll lock busy: another operation in progress for this channel")

// underEnrollLock runs fn only when this replica wins the channel's enroll
// lock, so exactly one replica works an enable/disable/reconnect at a time.
// Losing the race naks instead of acking: the holder may be running a
// DIFFERENT operation (back-to-back dashboard disconnect → enable lands the
// enable while the disable still holds the lock), so dropping the loser
// silently discards the newer intent — the disconnect/reconnect that "needed
// two clicks". Redelivery re-runs the job once the lock frees; a job made
// redundant by then (a true duplicate of the same op) is absorbed by the
// enroll cooldown / idempotent Helix calls instead of costing real budget.
func (w *Worker) underEnrollLock(ctx context.Context, op string, e enrollment, fn func() error) error {
	got, err := w.registry.AcquireEnrollLock(ctx, e.broadcasterID, w.owner, 60*time.Second)
	if err != nil {
		return err // transient valkey error: nak, let paced redelivery retry
	}
	if !got {
		w.log.Info(op+" waiting on enroll lock, nak for paced redelivery",
			zap.String("broadcaster_id", e.broadcasterID))
		return errEnrollLockBusy
	}
	defer func() { _ = w.registry.ReleaseEnrollLock(ctx, e.broadcasterID, w.owner) }()

	return fn()
}

// skipFreshEnroll acknowledges a redundant enroll: the channel completed a
// full enroll inside the cooldown window and the registry still reports it
// healthy, so re-running would only spend Helix budget re-creating (409) what
// already exists — the signature of a user spamming the dashboard's restart
// button. Any read error fails open and runs the enroll; wrongly skipping a
// repair is worse than wrongly spending budget.
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

// redundantEnroll trusts the cooldown only while the registry still reports
// the enrollment healthy; any other state means there is real work to do.
func redundantEnroll(ch manage.Channel, found bool) bool {
	return found && ch.SubState == "ok"
}

// recordEnrollSuccess persists the healthy outcome and arms the cooldown that
// lets an immediately repeated enable/reconnect be skipped.
func (w *Worker) recordEnrollSuccess(ctx context.Context, e enrollment) {
	_ = w.registry.SetSubState(ctx, e.broadcasterID, "ok", "")
	_ = w.registry.ArmEnrollCooldown(ctx, e.broadcasterID, enrollCooldownTTL)
}

// retryTransient runs fn up to three times with a growing back-off, stopping
// early on success, a permanent rejection, or context cancellation.
func retryTransient(ctx context.Context, fn func() error) error {
	var lastErr error
	for attempt := 1; attempt <= 3; attempt++ {
		lastErr = fn()
		if lastErr == nil || isPermanent(lastErr) {
			return lastErr
		}
		select {
		case <-ctx.Done():
			return lastErr
		case <-time.After(time.Duration(attempt) * 500 * time.Millisecond):
		}
	}
	return lastErr
}

// enableEventSubs creates all of a channel's eventsub subscriptions. Unlike
// reconnect it skips the drop phase: a first-time or re-enable has nothing to
// delete, and the creates are 409-idempotent, so dropping first would only add a
// needless delete pass and reset Twitch's conduit routing propagation for the
// fresh channel.chat.message sub.
//
// It shares reconnect's resilience — single-flight enroll lock, bounded internal
// retry, persisted sub_state, and ack-on-failure — instead of relying on lane
// redelivery for retries. The outgress work-queue's short MaxAge purges a nacked
// job before a rate-limit or transient-Twitch retry budget is spent, so a plain
// nack here would silently drop the enrollment under an onboarding burst. Acking
// with a persisted "failing" state surfaces the problem to the dashboard instead.
func (w *Worker) enableEventSubs(ctx context.Context, e enrollment) error {
	return w.underEnrollLock(ctx, "enable", e, func() error {
		if w.skipFreshEnroll(ctx, e, "enable") {
			return nil
		}
		_ = w.registry.SetSubState(ctx, e.broadcasterID, "pending", "")

		err := retryTransient(ctx, func() error {
			return w.createAllEventSubs(ctx, e)
		})
		if err == nil {
			w.recordEnrollSuccess(ctx, e)
			w.log.Info("eventsub subscriptions created", zap.String("broadcaster_id", e.broadcasterID))
			// A channel enrolled mid-stream gets no stream.online for the session
			// already running, so resolve the live state now instead of leaving
			// live-gated commands offline until the next stream.
			w.seedLiveStatus(ctx, e.broadcasterID)
			return nil
		}

		_ = w.registry.SetSubState(ctx, e.broadcasterID, "failing", err.Error())
		w.log.Error("enable: eventsubs not fully accepted, marked failing",
			zap.String("broadcaster_id", e.broadcasterID),
			zap.Error(err))
		return nil // ack: failing state is surfaced for the operator
	})
}

// disableChannel deletes all of a channel's eventsub subscriptions with the same
// resilience as enable/reconnect: single-flight enroll lock, bounded internal
// retry, and ack-on-failure with a persisted state, so a transient rate-limit or
// Twitch error is retried in-process instead of relying on lane redelivery. It
// wraps the raw disableEventSubs, which reconnect also calls directly (without
// the lock, inside its own single-flight section).
func (w *Worker) disableChannel(ctx context.Context, e enrollment) error {
	return w.underEnrollLock(ctx, "disable", e, func() error {
		err := retryTransient(ctx, func() error {
			return w.disableEventSubs(ctx, e)
		})
		if err == nil {
			// Cleared: no active enrollment is left to report health on, so a later
			// re-enable starts clean instead of inheriting a stale ok/failing state.
			_ = w.registry.SetSubState(ctx, e.broadcasterID, "", "")
			return nil
		}

		_ = w.registry.SetSubState(ctx, e.broadcasterID, "failing", err.Error())
		w.log.Error("disable: eventsubs not fully removed, marked failing",
			zap.String("broadcaster_id", e.broadcasterID),
			zap.Error(err))
		return nil // ack: failing state surfaced; leftovers converge on next reconnect/disable
	})
}

// reconnectEventSubs performs an atomic drop-then-recreate of all eventsub
// subscriptions for one broadcaster, single-flighted on the enroll lock. The
// recreate phase is retried for transient errors. Outcome is persisted to the
// registry (pending -> ok | failing) so the dashboard can surface it without
// polling Twitch.
func (w *Worker) reconnectEventSubs(ctx context.Context, e enrollment) error {
	return w.underEnrollLock(ctx, "reconnect", e, func() error {
		if w.skipFreshEnroll(ctx, e, "reconnect") {
			return nil
		}
		_ = w.registry.SetSubState(ctx, e.broadcasterID, "pending", "")

		// Best-effort drop: if listing/deleting fails we still try to recreate;
		// 409 idempotency on create means the end state converges either way.
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
			// The rebuilt subscriptions missed any go-live that happened while the
			// channel was between sub sets; re-resolve the live state directly.
			w.seedLiveStatus(ctx, e.broadcasterID)
			return nil
		}

		_ = w.registry.SetSubState(ctx, e.broadcasterID, "failing", err.Error())
		w.log.Error("reconnect: eventsubs not fully accepted, retrying",
			zap.String("broadcaster_id", e.broadcasterID),
			zap.Error(err))
		return err
	})
}

// disableEventSubs removes every subscription this conduit owns for the
// broadcaster: collect the owned ids first, then delete them, paying the
// reserved system bucket per Helix call.
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

// ownedSubIDs pages through the broadcaster's subscriptions and returns the
// ids of those this conduit owns.
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

// ownedSub reports whether the broadcaster owns this subscription on our
// conduit. The list query (?user_id) also returns subs where this id is the
// condition's user_id/moderator, not the broadcaster: notably every channel's
// channel.chat.message carries the bot as user_id. Only subs this broadcaster
// actually owns may be deleted, or reconnecting the bot account would wipe
// every channel's chat subscription.
func ownedSub(sub twitch.EventSubEntry, e enrollment) bool {
	if sub.Transport.ConduitID != e.conduitID {
		return false
	}
	return sub.Condition.BroadcasterUserID == "" || sub.Condition.BroadcasterUserID == e.broadcasterID
}

// createAllEventSubs creates every SubSpec for the channel; returns the first
// error (with the failing subscription type) or nil when all are accepted
// (202 or 409-idempotent).
func (w *Worker) createAllEventSubs(ctx context.Context, e enrollment) error {
	if w.botID == "" {
		// chat sub cannot be built without a bot id; treat as a hard failure
		// because an all-or-nothing reconnect must not silently skip it.
		return fmt.Errorf("bot user id not configured: channel.chat.message cannot be created")
	}

	for _, spec := range twitch.ChannelSubscriptions(e.broadcasterID, w.botID) {
		if err := w.takeSystemHelix(ctx); err != nil {
			return err
		}
		if err := w.twitch.CreateEventSub(ctx, spec, e.conduitID); err != nil {
			w.conduit.Invalidate()
			return fmt.Errorf("create %s: %w", spec.Type, err)
		}
	}

	// Optional subscriptions (channel-points redemptions) ride along on enable;
	// a channel that has not consented to the channel-points scope simply skips
	// them (see createOptionalEventSubs) instead of failing the whole enroll.
	return w.createOptionalEventSubs(ctx, e)
}

// ensureOptionalEventSubs (re)creates only the optional subscriptions for a
// channel (e.g. the channel-points redemption sub after the broadcaster grants
// the channel:manage:redemptions scope), single-flighted on the enroll lock so
// only one replica works it. Missing optional subs are best-effort: a channel
// without the scope skips them cleanly.
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

// createOptionalEventSubs creates each optional SubSpec, paying the reserved
// system bucket per call. A permanent rejection (the channel has not granted
// the required scope) is logged and skipped rather than failing the enroll;
// only transient errors propagate for retry.
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

// eventSubFailure splits permanent rejections (bad request, missing consent
// scopes) from everything retryable. Permanent ones are dropped with a log
// line; retrying them would burn the whole redelivery budget for nothing.
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

// isPermanent reports whether err is a non-retryable Twitch rejection:
// any 4xx except 429 (rate limit) and 401 (auth may recover).
func isPermanent(err error) bool {
	var se *twitch.StatusError
	if errors.As(err, &se) {
		return se.Status >= 400 && se.Status < 500 &&
			se.Status != http.StatusTooManyRequests &&
			se.Status != http.StatusUnauthorized
	}
	return false
}
