package worker

import (
	"context"
	"errors"
	"time"

	"ItsBagelBot/app/outgress/internal/twitch"
	"ItsBagelBot/pkg/bus"
	"ItsBagelBot/pkg/monitor"

	"github.com/bytedance/sonic"

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
// a channel the registry knows, still enabled, and currently marked revoked
// or failing gets the enroll: a first-time consent has no registry entry yet
// (the dashboard enable owns that path), and a healthy channel needs nothing.
// The enable path is create-only (409-idempotent), so the surviving unscoped
// subscriptions are never dropped and recreated.
func (w *Worker) HandleAuthzGranted(msg *bus.Message) error {
	ctx := msg.Context()
	log := monitor.TxnLogger(ctx, w.log)

	var ev authzUser
	if err := sonic.Unmarshal(msg.Payload, &ev); err != nil || ev.UserID == "" {
		log.Error("dropping malformed authz.granted event", zap.Error(err))
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

	log.Info("authorization granted, re-enrolling eventsubs",
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
	case subStateRevoked, subStateFailing, subStatePending:
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
	log := monitor.TxnLogger(ctx, w.log)

	var ev authzUser
	if err := sonic.Unmarshal(msg.Payload, &ev); err != nil || ev.UserID == "" {
		log.Error("dropping malformed authz.revoked event", zap.Error(err))
		return nil
	}

	if ev.UserID == w.botID {
		// The bot account's own grant died: every channel's chat is affected
		// and no per-channel state captures that. Scream for the operator.
		log.Error("BOT ACCOUNT authorization revoked, chat send and chat reads will fail until the bot re-authorizes",
			zap.String("bot_id", w.botID))
		noticeError(ctx, errBotAuthRevoked)
		return nil
	}

	return w.markAuthorizationRevoked(ctx, ev.UserID, "authorization_revoked")
}

// HandleAuthzSubRevoked folds a single-subscription revocation into the
// channel state. Consent-shaped statuses mark the channel revoked; anything
// else (version_removed, notification_failures_exceeded) is a bot-side fault
// and marks it failing so the operator sees it.
func (w *Worker) HandleAuthzSubRevoked(msg *bus.Message) error {
	ctx := msg.Context()
	log := monitor.TxnLogger(ctx, w.log)

	var ev authzSubRevoked
	if err := sonic.Unmarshal(msg.Payload, &ev); err != nil || ev.BroadcasterID == "" {
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

	if !consentRevokedStatus(ev.Status) {
		return w.markSubDropped(ctx, ev)
	}
	return w.markAuthorizationRevoked(ctx, ev.BroadcasterID, ev.Status+": "+ev.Type)
}

// consentRevokedStatus reports whether a revocation status means the
// broadcaster's consent is gone (as opposed to a bot-side subscription
// fault).
func consentRevokedStatus(status string) bool {
	return status == "authorization_revoked" || status == "user_removed"
}

// markAuthorizationRevoked flips one channel to the revoked state and
// notifies the streamer, exactly once per outage: repeat events (Twitch sends
// one revocation per subscription) see the state already revoked and stop.
func (w *Worker) markAuthorizationRevoked(ctx context.Context, broadcasterID, reason string) error {
	ch, found, err := w.registry.Get(ctx, broadcasterID)
	if err != nil {
		return err // transient registry read: nak for paced redelivery
	}
	if !found || ch.SubState == subStateRevoked {
		return nil
	}

	if err := w.registry.SetSubState(ctx, broadcasterID, subStateRevoked, reason); err != nil {
		return err
	}
	w.log.Warn("broadcaster authorization revoked, channel marked for reconnect",
		zap.String("broadcaster_id", broadcasterID),
		zap.String("reason", reason))

	w.notifyReauthNeeded(ctx, broadcasterID)
	return nil
}

// markSubDropped records a non-consent revocation (version_removed,
// notification_failures_exceeded) as a failing enrollment so the dashboard
// offers the restart that re-creates it.
func (w *Worker) markSubDropped(ctx context.Context, ev authzSubRevoked) error {
	ch, found, err := w.registry.Get(ctx, ev.BroadcasterID)
	if err != nil {
		return err
	}
	// A channel already flagged revoked keeps that stronger state: it also
	// explains the dropped subscription, and the reconnect CTA would fail
	// until the broadcaster re-consents anyway.
	if !found || ch.SubState == subStateRevoked {
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

// notifyReauthNeeded fans the revocation out to the streamer-facing channels
// (dashboard bell notification). Best-effort: state is already persisted, and
// the go-live chat beacon still fires later regardless.
func (w *Worker) notifyReauthNeeded(ctx context.Context, broadcasterID string) {
	if w.reauth == nil {
		return
	}
	w.reauth.Notify(ctx, broadcasterID, noticeRevoked)
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
