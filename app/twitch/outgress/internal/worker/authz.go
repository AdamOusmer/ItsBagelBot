// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package worker

import (
	"context"
	"errors"
	"time"

	"ItsBagelBot/app/twitch/outgress/internal/twitch"
	"ItsBagelBot/internal/domain/rpc/manage"
	"ItsBagelBot/pkg/bus"
	"ItsBagelBot/pkg/codec"
	"ItsBagelBot/pkg/monitor"

	"go.uber.org/zap"
)

// errBotAuthRevoked names the fleet-level failure where the bot account's own
// authorization died; it exists so New Relic groups these apart from
// per-channel noise.
var errBotAuthRevoked = errors.New("bot account authorization revoked")

// Authorization lifecycle events off the ingress status subjects. Ingress
// publishes them when Twitch reports an authorization change for our client:
//
//   - authz.granted: a user (re)consented through the OAuth flow
//     (user.authorization.grant).
//   - authz.revoked: a user's authorization died (user.authorization.revoke):
//     they disconnected the app or changed their password.
//   - authz.subrevoked: Twitch revoked one concrete subscription, with the
//     status naming why (authorization_revoked, user_removed,
//     version_removed, ...).
//
// The policy encoded here, deliberately: a revocation only MARKS the channel
// (sub_state "revoked") and notifies the streamer. It never deletes the
// subscriptions Twitch left alive: stream.online/offline survive without any
// user grant, and go-live is the beacon that reaches the streamer in chat.
// Re-enrollment happens exactly when Twitch says the consent is back (the
// grant event), not on our own login flow's schedule.

// authzUser is the wire payload of authz.granted / authz.revoked.
type authzUser struct {
	UserID    string `json:"user_id"`
	UserLogin string `json:"user_login"`
}

// authzSubRevoked is the wire payload of authz.subrevoked.
type authzSubRevoked struct {
	BroadcasterID string `json:"broadcaster_id"`
	Type          string `json:"type"`
	Status        string `json:"status"`
}

// HandleAuthzGranted re-enrolls a channel whose consent just came back. Only
// a channel the registry knows, still enabled, and currently marked revoked,
// banned or failing gets the enroll: a first-time consent has no registry entry yet
// (the dashboard enable owns that path), and a healthy channel needs nothing.
// The enable path is create-only (409-idempotent), so the surviving unscoped
// subscriptions are never dropped and recreated.
func (w *Worker) HandleAuthzGranted(msg *bus.Message) error {
	ctx := msg.Context()
	ev, ok := w.decodeAuthzUser(ctx, msg)
	if !ok {
		return nil
	}

	ch, found, err := w.registry.Get(ctx, ev.UserID)
	if err != nil {
		return err // transient registry read: nak for paced redelivery
	}
	if !found || !ch.Enabled {
		return nil
	}

	// Clear the grant marker BEFORE the re-enroll gate below. A grant that died
	// without being revoked leaves sub_state "ok", which reenrollableSubState
	// rejects, so a clear placed after it would never run and the streamer who
	// just did what the chat line asked would be nagged forever.
	w.clearGrantDead(ctx, ev.UserID, ch)

	if !reenrollableSubState(ch.SubState) {
		return nil
	}
	return w.reenrollAfterGrant(ctx, ev, ch)
}

// decodeAuthzUser unmarshals an authz.granted / authz.revoked payload. A
// malformed or empty event is logged and dropped (ok=false), never nakked:
// redelivery cannot repair bad bytes.
func (w *Worker) decodeAuthzUser(ctx context.Context, msg *bus.Message) (authzUser, bool) {
	var ev authzUser
	if err := codec.Unmarshal(msg.Payload, &ev); err != nil || ev.UserID == "" {
		monitor.TxnLogger(ctx, w.log).Error("dropping malformed authz user event",
			zap.String("uuid", msg.UUID), zap.Error(err))
		return authzUser{}, false
	}
	return ev, true
}

// reenrollAfterGrant is the repair half of HandleAuthzGranted: reactivate a
// channel blockChannel deactivated, then run the create-only enroll.
func (w *Worker) reenrollAfterGrant(ctx context.Context, ev authzUser, ch manage.Channel) error {
	// blockChannel deactivated the row; consent is back, so the channel is
	// served again from here whatever the enroll below reports. A channel the
	// streamer disconnected on purpose has an empty state and never reaches
	// this line, so their choice is not overridden.
	if blockedChannel(ch) {
		w.setChannelActive(ctx, ev.UserID, true)
	}

	monitor.TxnLogger(ctx, w.log).Info("authorization granted, re-enrolling eventsubs",
		zap.String("broadcaster_id", ev.UserID),
		zap.String("user_login", ev.UserLogin),
		zap.String("prior_sub_state", ch.SubState))

	conduitID, err := w.conduit.Get(ctx)
	if err != nil {
		return err
	}
	// Leave the revoked guard: the enable path skips revoked channels, so the
	// state steps to pending first. enableEventSubs acks failures into the
	// persisted state itself, so this handler never loops on a bad channel.
	_ = w.registry.SetSubState(ctx, ev.UserID, subStatePending, "")
	return w.enableEventSubs(ctx, enrollment{broadcasterID: ev.UserID, conduitID: conduitID})
}

// reenrollableSubState reports whether a grant event should trigger a fresh
// enroll. Revoked is the designed case; failing rides along because a consent
// refresh is exactly what a consent-shaped failure needs; pending keeps a
// nakked grant retryable after this handler already stepped the state (the
// creates converge through 409s at worst). Only a healthy "ok" (or an
// unenrolled channel) skips.
func reenrollableSubState(state string) bool {
	switch state {
	case subStateRevoked, subStateBanned, subStateFailing, subStatePending:
		return true
	}
	return false
}

// HandleAuthzRevoked marks a channel revoked when Twitch reports the
// broadcaster's authorization died. No Twitch calls: the scoped subscriptions
// are already gone server-side, and the survivors are kept as the go-live
// beacon.
func (w *Worker) HandleAuthzRevoked(msg *bus.Message) error {
	ctx := msg.Context()
	ev, ok := w.decodeAuthzUser(ctx, msg)
	if !ok {
		return nil
	}

	if ev.UserID == w.botID {
		// The bot account's own grant died: every channel's chat is affected
		// and no per-channel state captures that. Scream for the operator.
		monitor.TxnLogger(ctx, w.log).Error("BOT ACCOUNT authorization revoked, chat send and chat reads will fail until the bot re-authorizes",
			zap.String("bot_id", w.botID))
		noticeError(ctx, errBotAuthRevoked)
		return nil
	}

	return w.blockChannel(ctx, ev.UserID, blockRevoked.because("authorization_revoked"))
}

// HandleAuthzSubRevoked folds a single-subscription revocation into the
// channel state. Consent-shaped statuses mark the channel revoked;
// chat_user_banned marks it banned (the chat banned the bot, consent is
// intact); anything else (version_removed, notification_failures_exceeded)
// is a bot-side fault and marks it failing so the operator sees it.
func (w *Worker) HandleAuthzSubRevoked(msg *bus.Message) error {
	ctx := msg.Context()
	log := monitor.TxnLogger(ctx, w.log)

	var ev authzSubRevoked
	if err := codec.Unmarshal(msg.Payload, &ev); err != nil || ev.BroadcasterID == "" {
		log.Error("dropping malformed authz.subrevoked event", zap.Error(err))
		return nil
	}

	if ev.BroadcasterID == w.botID {
		// The bot appears as condition.user_id on every channel's chat sub;
		// a per-channel state write would target the wrong entity.
		log.Error("subscription carrying the bot's own authorization revoked",
			zap.String("type", ev.Type), zap.String("status", ev.Status))
		return nil
	}

	reason := ev.Status + ": " + ev.Type
	switch {
	case consentRevokedStatus(ev.Status):
		return w.blockChannel(ctx, ev.BroadcasterID, blockRevoked.because(reason))
	case ev.Status == statusChatUserBanned:
		return w.blockChannel(ctx, ev.BroadcasterID, blockBanned.because(reason))
	default:
		return w.markSubDropped(ctx, ev)
	}
}

// statusChatUserBanned is Twitch's revocation status when the user_id of a
// chat subscription (our bot) is banned from the broadcaster's chat.
const statusChatUserBanned = "chat_user_banned"

// consentRevokedStatus reports whether a revocation status means the
// broadcaster's consent is gone (as opposed to a bot-side subscription
// fault).
func consentRevokedStatus(status string) bool {
	return status == "authorization_revoked" || status == "user_removed"
}

// blockade is one streamer-fixable reason the bot cannot serve a channel:
// the registry state that names it, the notice that tells the streamer the
// matching remedy, and the reason persisted beside the state. State and
// notice are surfaced verbatim by the dashboard, the admin console and the
// bell, so the two never share copy.
type blockade struct {
	state  string
	notice notice
	reason string
}

var (
	blockRevoked = blockade{state: subStateRevoked, notice: noticeRevoked}
	blockBanned  = blockade{state: subStateBanned, notice: noticeBanned}
)

// because returns the blockade with the reason the registry will record.
func (b blockade) because(reason string) blockade {
	b.reason = reason
	return b
}

// blockChannel flips one channel to a blocked state, deactivates it and
// notifies the streamer, exactly once per outage: repeat events (Twitch sends
// one revocation per subscription) see the state already set and stop.
// Revoked is the stronger statement and never downgrades to banned.
//
// Deactivation is what drops the channel out of the admin's active count and
// the ingress's traffic: an inactive row is the users service's own notion of
// "the bot is not serving this broadcaster", and until the streamer acts that
// is the truth. The grant path (or the streamer's Enable) reactivates it.
func (w *Worker) blockChannel(ctx context.Context, broadcasterID string, b blockade) error {
	ch, found, err := w.registry.Get(ctx, broadcasterID)
	if err != nil {
		return err // transient registry read: nak for paced redelivery
	}
	if !found || alreadyBlocked(ch, b) {
		return nil
	}

	if err := w.registry.SetSubState(ctx, broadcasterID, b.state, b.reason); err != nil {
		return err
	}
	w.log.Warn("channel blocked until the streamer acts",
		zap.String("broadcaster_id", broadcasterID),
		zap.String("state", b.state),
		zap.String("reason", b.reason))

	w.setChannelActive(ctx, broadcasterID, false)
	if w.reauth != nil {
		w.reauth.Notify(ctx, broadcasterID, b.notice)
	}
	return nil
}

// alreadyBlocked reports whether applying b to ch would change nothing:
// the state is already b, or the stronger revoked state is already set.
func alreadyBlocked(ch manage.Channel, b blockade) bool {
	return ch.SubState == b.state || ch.SubState == subStateRevoked
}

// blockedChannel reports whether the channel sits in a state blockChannel set.
func blockedChannel(ch manage.Channel) bool {
	return ch.SubState == subStateRevoked || ch.SubState == subStateBanned
}

// setChannelActive flips the users-service active flag. Best-effort: the
// registry state is the source of truth for the enroll machinery, and the
// consoles read that state ahead of the flag, so a lost flip costs only the
// count and the ingress traffic gate until the next transition.
func (w *Worker) setChannelActive(ctx context.Context, broadcasterID string, active bool) {
	if w.reauth == nil {
		return
	}
	if err := w.reauth.SetActive(ctx, broadcasterID, active); err != nil {
		w.log.Warn("channel active flag not updated",
			zap.String("broadcaster_id", broadcasterID),
			zap.Bool("active", active),
			zap.Error(err))
	}
}

// markSubDropped records a non-consent revocation (version_removed,
// notification_failures_exceeded) as a failing enrollment so the dashboard
// offers the restart that re-creates it.
func (w *Worker) markSubDropped(ctx context.Context, ev authzSubRevoked) error {
	ch, found, err := w.registry.Get(ctx, ev.BroadcasterID)
	if err != nil {
		return err
	}
	// A channel already blocked keeps that stronger state: it also explains
	// the dropped subscription, and the reconnect CTA would fail until the
	// broadcaster re-consents (or unbans the bot) anyway.
	if !found || blockedChannel(ch) {
		return nil
	}

	if err := w.registry.SetSubState(ctx, ev.BroadcasterID, subStateFailing, ev.Status+": "+ev.Type); err != nil {
		return err
	}
	w.log.Error("eventsub subscription revoked for a bot-side fault",
		zap.String("broadcaster_id", ev.BroadcasterID),
		zap.String("type", ev.Type),
		zap.String("status", ev.Status))
	return nil
}

// EnsureClientEventSubs creates the client-scoped authorization subscriptions
// on the conduit (409-idempotent), retrying with backoff until they exist or
// ctx ends. Runs once per boot in the background: without these, revocations
// and re-grants only surface through per-subscription revocation messages and
// enroll failures.
func (w *Worker) EnsureClientEventSubs(ctx context.Context) {
	clientID := w.twitch.ClientID()
	if clientID == "" {
		w.log.Warn("client id not configured, skipping user.authorization subscriptions")
		return
	}

	for attempt := 1; ctx.Err() == nil; attempt++ {
		if w.tryCreateClientEventSubs(ctx, clientID, attempt) {
			return
		}
		if !sleepCtx(ctx, backoffDelay(attempt)) {
			return
		}
	}
}

// tryCreateClientEventSubs runs one create pass and reports whether the
// subscriptions are in place; a failure is logged for the retry loop unless
// the context already ended (shutdown is not an error worth a warning).
func (w *Worker) tryCreateClientEventSubs(ctx context.Context, clientID string, attempt int) bool {
	err := w.createClientEventSubs(ctx, clientID)
	if err == nil {
		w.log.Info("user.authorization eventsubs ensured on conduit")
		return true
	}
	if ctx.Err() == nil {
		w.log.Warn("ensuring user.authorization eventsubs failed, will retry",
			zap.Int("attempt", attempt), zap.Error(err))
	}
	return false
}

// sleepCtx pauses for d, reporting false when ctx ended first.
func sleepCtx(ctx context.Context, d time.Duration) bool {
	select {
	case <-ctx.Done():
		return false
	case <-time.After(d):
		return true
	}
}

func (w *Worker) createClientEventSubs(ctx context.Context, clientID string) error {
	conduitID, err := w.conduit.Get(ctx)
	if err != nil {
		return err
	}
	for _, spec := range twitch.ClientSubscriptions(clientID) {
		if err := w.takeSystemHelix(ctx); err != nil {
			return err
		}
		if err := w.twitch.CreateEventSub(ctx, spec, conduitID); err != nil {
			w.conduit.Invalidate()
			return err
		}
	}
	return nil
}

// backoffDelay grows linearly to a one-minute ceiling; boot-time conduit
// resolution is the only expected transient here.
func backoffDelay(attempt int) time.Duration {
	d := time.Duration(attempt) * 5 * time.Second
	if d > time.Minute {
		return time.Minute
	}
	return d
}
