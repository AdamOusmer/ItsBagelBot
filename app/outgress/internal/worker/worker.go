// Package worker drains one outgress lane: it enforces the channel registry,
// the Twitch rate limits, and the premium reservation, then executes the
// Helix request. Handlers nack on anything retryable and rely on the lane
// subscriber's paced redelivery, so a rate-limited or failing message waits
// out its budget instead of spinning.
package worker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"ItsBagelBot/app/outgress/internal/channels"
	"ItsBagelBot/app/outgress/internal/conduit"
	"ItsBagelBot/app/outgress/internal/ratelimit"
	"ItsBagelBot/app/outgress/internal/twitch"
	"ItsBagelBot/internal/domain/outgress"
	"ItsBagelBot/internal/domain/rpc/manage"

	"github.com/ThreeDotsLabs/watermill/message"

	"go.uber.org/zap"
)

// Twitch enforces the chat limits per channel (20 messages per 30s, 100 when
// the bot moderates the channel) and one Helix budget per app token (800 per
// minute, shared by every endpoint the app calls).
//
// That 800/min budget is partitioned so the lanes cannot starve each other:
//
//	helixSystemReserve   tokens/min are reserved for the system lane (the
//	                     dashboard's EventSub create/delete jobs), drawn from
//	                     ratelimit:helix:system and nothing else. Reserving
//	                     them means an onboarding burst always has capacity,
//	                     and capping the lane to them means a flood of toggles
//	                     can never drain the budget chat/api traffic needs.
//	helixGeneralCapacity tokens/min (the remainder) back ordinary api traffic
//	                     on ratelimit:helix:app. The standard lane is held to
//	                     half of that by its own bucket, so premium api always
//	                     finds at least half of the general budget free.
//
// The two partitions are disjoint and sum to the real limit, so the fleet
// never exceeds 800/min no matter how the lanes mix.
const (
	chatCapacity    = 20.0
	chatModCapacity = 100.0
	chatWindow      = 30.0

	helixCapacity        = 800.0
	helixWindow          = 60.0
	helixSystemReserve   = 100.0
	helixGeneralCapacity = helixCapacity - helixSystemReserve

	// modStatusTTL is how long a verified mod status is trusted before the
	// worker re-checks it against Twitch.
	modStatusTTL = time.Hour
)

// Lane identifies which queue a worker drains; it selects the rate-limit
// buckets the worker pays into.
type Lane int

const (
	LanePremium Lane = iota
	LaneStandard
	LaneSystem
)

// ErrPaused nacks every message while the kill switch is on; redelivery
// pacing holds them until resume or until their retry budget runs out.
var ErrPaused = errors.New("outgress is paused")

// helixRoute is the Helix call a message type maps to when the producer leaves
// endpoint/method empty. as is the default token identity for the type ("" =
// route by endpoint), applied only when the message does not set its own.
type helixRoute struct {
	method   string
	endpoint string
	as       string
}

// typeRoutes lets outgress own the Helix endpoint per type, so producers send
// intent ("chat", "ban", "ad", "clip") plus the body instead of hardcoding
// paths. Types absent here (e.g. "api") are generic passthroughs and must carry
// their own endpoint.
//
//   - chat:        bot send (app token honors user:bot + channel:bot).
//   - ban/unban:   bot acts as moderator (/helix/moderation/* → bot user token).
//   - timeout:     same endpoint as ban; the body's duration makes it a timeout.
//   - ad/commercial: the broadcaster starts the ad (channel:edit:commercial).
//   - clip:        the broadcaster's grant creates the clip (clips:edit).
var typeRoutes = map[string]helixRoute{
	outgress.TypeChat:       {http.MethodPost, "/helix/chat/messages", ""},
	outgress.TypeBan:        {http.MethodPost, "/helix/moderation/bans", outgress.AsBot},
	outgress.TypeTimeout:    {http.MethodPost, "/helix/moderation/bans", outgress.AsBot},
	outgress.TypeUnban:      {http.MethodDelete, "/helix/moderation/bans", outgress.AsBot},
	outgress.TypeAd:         {http.MethodPost, "/helix/channels/commercial", outgress.AsBroadcaster},
	outgress.TypeCommercial: {http.MethodPost, "/helix/channels/commercial", outgress.AsBroadcaster},
	outgress.TypeClip:       {http.MethodPost, "/helix/clips", outgress.AsBroadcaster},
}

type Worker struct {
	log      *zap.Logger
	limiter  *ratelimit.Limiter
	registry *channels.Registry
	twitch   *twitch.Client
	botID    string
	owner    string // pod identity for the enroll lock (os.Hostname)
	conduit  *conduit.Resolver
	lane     Lane
}

func New(log *zap.Logger, limiter *ratelimit.Limiter, registry *channels.Registry, tw *twitch.Client, botID, owner string, conduitResolver *conduit.Resolver, lane Lane) *Worker {
	return &Worker{
		log:      log,
		limiter:  limiter,
		registry: registry,
		twitch:   tw,
		botID:    botID,
		owner:    owner,
		conduit:  conduitResolver,
		lane:     lane,
	}
}

func (w *Worker) Process(msg *message.Message) error {

	var payload outgress.Message
	if err := json.Unmarshal(msg.Payload, &payload); err != nil {
		w.log.Error("dropping malformed outgress message", zap.Error(err))
		return nil
	}

	ctx := msg.Context()

	paused, err := w.registry.Paused(ctx)
	if err != nil {
		return err
	}
	if paused {
		return ErrPaused
	}

	if payload.Type == outgress.TypeEventSub {
		return w.processEventSub(ctx, payload)
	}

	// Helix path: "chat", "api", and the mapped intents (ban, unban, ad, clip…).
	// Fill endpoint/method/as from the type when the producer left them empty, so
	// a job only needs its intent + body. "api" has no mapping (generic
	// passthrough) and must carry its own endpoint. An explicit field always
	// wins, so any default can be overridden.
	_, mapped := typeRoutes[payload.Type]
	if payload.Type != outgress.TypeChat && payload.Type != outgress.TypeAPI && !mapped {
		w.log.Error("dropping message with unknown type", zap.String("type", payload.Type))
		return nil
	}
	if r, ok := typeRoutes[payload.Type]; ok {
		if payload.Endpoint == "" {
			payload.Endpoint = r.endpoint
		}
		if payload.Method == "" {
			payload.Method = r.method
		}
		if payload.As == "" {
			payload.As = r.as
		}
	}
	if !strings.HasPrefix(payload.Endpoint, "/helix/") || payload.Method == "" {
		w.log.Error("dropping message with invalid request",
			zap.String("type", payload.Type),
			zap.String("endpoint", payload.Endpoint),
			zap.String("method", payload.Method))
		return nil
	}

	// Only "chat" pays the chat rate buckets; every other Helix call pays the
	// general bucket.
	if payload.Type == outgress.TypeChat {
		return w.processChat(ctx, payload)
	}
	return w.processAPI(ctx, payload)
}

func (w *Worker) processChat(ctx context.Context, payload outgress.Message) error {

	ch, found, err := w.registry.Get(ctx, payload.BroadcasterID)
	if err != nil {
		return err
	}
	if found && !ch.Enabled {
		w.log.Info("dropping message for disabled channel", zap.String("broadcaster_id", payload.BroadcasterID))
		return nil
	}

	capacity := chatCapacity
	if w.modStatus(ctx, payload, ch, found) {
		capacity = chatModCapacity
	}
	refill := capacity / chatWindow

	// The standard lane pays its restricted bucket first: if the shared
	// bucket then rejects, the wasted token only makes standard traffic more
	// conservative, while the reverse order would let it drain tokens the
	// premium lane is entitled to.
	if w.lane == LaneStandard {
		if err := w.take(ctx, ratelimit.Bucket{
			Key:             "ratelimit:chat:standard:" + payload.BroadcasterID,
			Capacity:        capacity / 2,
			RefillPerSecond: refill / 2,
		}); err != nil {
			return err
		}
	}

	if err := w.take(ctx, ratelimit.Bucket{
		Key:             "ratelimit:chat:" + payload.BroadcasterID,
		Capacity:        capacity,
		RefillPerSecond: refill,
	}); err != nil {
		return err
	}

	return w.execute(ctx, payload)
}

func (w *Worker) processAPI(ctx context.Context, payload outgress.Message) error {

	if w.lane == LaneStandard {
		if err := w.take(ctx, ratelimit.Bucket{
			Key:             "ratelimit:helix:app:standard",
			Capacity:        helixGeneralCapacity / 2,
			RefillPerSecond: helixGeneralCapacity / helixWindow / 2,
		}); err != nil {
			return err
		}
	}

	if err := w.takeGeneralHelix(ctx); err != nil {
		return err
	}

	return w.execute(ctx, payload)
}

// processEventSub applies the receive toggle for one channel, paying the
// reserved system Helix bucket once per HTTP call. Conduit EventSub
// management runs under the app token. Chat (channel.chat.message) is read in
// the bot account's user context (the bot's user:read:chat / user:bot grant
// plus the broadcaster's channel:bot grant); the broadcaster events (subs,
// cheers, follows, channel.update title changes) are authorized by the
// broadcaster's own consent (channel:read:subscriptions, bits:read,
// moderator:read:followers). No bot user token is involved here. Creates are 409-idempotent and deletes
// 404-idempotent, so a job nacked halfway (rate limit, transient Twitch
// error) converges when redelivery re-runs it.
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
	if err := json.Unmarshal(payload.Payload, &job); err != nil {
		w.log.Error("dropping malformed eventsub job", zap.Error(err))
		return nil
	}

	// Resolve effective mode: explicit Mode wins; empty falls back to legacy Enabled field.
	mode := job.Mode
	if mode == "" {
		if job.Enabled {
			mode = outgress.ModeEnable
		} else {
			mode = outgress.ModeDisable
		}
	}

	switch mode {
	case outgress.ModeEnable:
		return w.enableEventSubs(ctx, payload.BroadcasterID, conduitID)
	case outgress.ModeDisable:
		return w.disableEventSubs(ctx, payload.BroadcasterID, conduitID)
	case outgress.ModeReconnect:
		return w.reconnectEventSubs(ctx, payload.BroadcasterID, conduitID)
	default:
		w.log.Error("dropping eventsub job with unknown mode",
			zap.String("mode", mode),
			zap.String("broadcaster_id", payload.BroadcasterID))
		return nil
	}
}

func (w *Worker) enableEventSubs(ctx context.Context, broadcasterID, conduitID string) error {

	if w.botID == "" {
		w.log.Warn("bot user id not configured, channel chat subscription will be skipped",
			zap.String("broadcaster_id", broadcasterID))
	}

	for _, spec := range twitch.ChannelSubscriptions(broadcasterID, w.botID) {
		if err := w.takeSystemHelix(ctx); err != nil {
			return err
		}
		if err := w.twitch.CreateEventSub(ctx, spec, conduitID); err != nil {
			w.conduit.Invalidate()
			return w.eventSubFailure(err, "eventsub create", broadcasterID, spec.Type)
		}
	}

	w.log.Info("eventsub subscriptions created", zap.String("broadcaster_id", broadcasterID))
	return nil
}

func (w *Worker) disableEventSubs(ctx context.Context, broadcasterID, conduitID string) error {

	deleted := 0
	cursor := ""
	for {
		if err := w.takeSystemHelix(ctx); err != nil {
			return err
		}
		subs, next, err := w.twitch.ListEventSubs(ctx, broadcasterID, cursor)
		if err != nil {
			return w.eventSubFailure(err, "eventsub list", broadcasterID, "")
		}

		for _, sub := range subs {
			if sub.Transport.ConduitID != conduitID {
				continue
			}
			// The list query (?user_id) also returns subs where this id is the
			// condition's user_id/moderator, not the broadcaster: notably every
			// channel's channel.chat.message carries the bot as user_id. Only
			// delete subs this broadcaster actually owns, or reconnecting the bot
			// account would wipe every channel's chat subscription.
			if sub.Condition.BroadcasterUserID != "" && sub.Condition.BroadcasterUserID != broadcasterID {
				continue
			}
			if err := w.takeSystemHelix(ctx); err != nil {
				return err
			}
			if err := w.twitch.DeleteEventSub(ctx, sub.ID); err != nil {
				return w.eventSubFailure(err, "eventsub delete", broadcasterID, "")
			}
			deleted++
		}

		if next == "" {
			break
		}
		cursor = next
	}

	w.log.Info("eventsub subscriptions removed",
		zap.String("broadcaster_id", broadcasterID), zap.Int("deleted", deleted))
	return nil
}

// eventSubFailure splits permanent rejections (bad request, missing consent
// scopes) from everything retryable. Permanent ones are dropped with a log
// line; retrying them would burn the whole redelivery budget for nothing.
func (w *Worker) eventSubFailure(err error, op, broadcasterID, subType string) error {

	var status *twitch.StatusError
	if errors.As(err, &status) &&
		status.Status >= 400 && status.Status < 500 &&
		status.Status != http.StatusTooManyRequests &&
		status.Status != http.StatusUnauthorized {
		w.log.Error("dropping eventsub job twitch rejected",
			zap.String("op", op),
			zap.String("broadcaster_id", broadcasterID),
			zap.String("subscription", subType),
			zap.Error(err))
		return nil
	}

	w.log.Warn("eventsub job failed, will retry",
		zap.String("op", op),
		zap.String("broadcaster_id", broadcasterID),
		zap.Error(err))
	return err
}

// reconnectEventSubs performs an atomic drop-then-recreate of all eventsub
// subscriptions for one broadcaster. It acquires a Valkey single-flight lock
// so only one replica works the reconnect; others ack and return immediately.
// The recreate phase is retried up to 3 times for transient errors. Outcome is
// persisted to the registry (pending -> ok | failing) so the dashboard can
// surface it without polling Twitch.
func (w *Worker) reconnectEventSubs(ctx context.Context, broadcasterID, conduitID string) error {

	got, err := w.registry.AcquireEnrollLock(ctx, broadcasterID, w.owner, 60*time.Second)
	if err != nil {
		return err // transient valkey error: nak, let paced redelivery retry
	}
	if !got {
		w.log.Info("reconnect already in progress on another replica",
			zap.String("broadcaster_id", broadcasterID))
		return nil // ack: another replica owns it
	}
	defer func() { _ = w.registry.ReleaseEnrollLock(ctx, broadcasterID, w.owner) }()

	_ = w.registry.SetSubState(ctx, broadcasterID, "pending", "")

	// Best-effort drop: if listing/deleting fails we still try to recreate;
	// 409 idempotency on create means the end state converges either way.
	if derr := w.disableEventSubs(ctx, broadcasterID, conduitID); derr != nil {
		w.log.Warn("reconnect: drop phase failed, proceeding to recreate",
			zap.String("broadcaster_id", broadcasterID),
			zap.Error(derr))
	}

	var lastErr error
	for attempt := 1; attempt <= 3; attempt++ {
		lastErr = w.createAllEventSubs(ctx, broadcasterID, conduitID)
		if lastErr == nil {
			_ = w.registry.SetSubState(ctx, broadcasterID, "ok", "")
			w.log.Info("reconnect: all eventsubs accepted",
				zap.String("broadcaster_id", broadcasterID))
			return nil
		}
		if isPermanent(lastErr) {
			break // 403 etc: retrying will not help
		}
		// transient: small back-off before next attempt
		select {
		case <-ctx.Done():
			break
		case <-time.After(time.Duration(attempt) * 500 * time.Millisecond):
		}
	}

	_ = w.registry.SetSubState(ctx, broadcasterID, "failing", lastErr.Error())
	w.log.Error("reconnect: eventsubs not fully accepted, marked failing",
		zap.String("broadcaster_id", broadcasterID),
		zap.Error(lastErr))
	return nil // ack: failing state is surfaced for the operator
}

// createAllEventSubs creates every SubSpec for the channel; returns the first
// error (with the failing subscription type) or nil when all are accepted
// (202 or 409-idempotent).
func (w *Worker) createAllEventSubs(ctx context.Context, broadcasterID, conduitID string) error {

	if w.botID == "" {
		// chat sub cannot be built without a bot id; treat as a hard failure
		// because an all-or-nothing reconnect must not silently skip it.
		return fmt.Errorf("bot user id not configured: channel.chat.message cannot be created")
	}

	for _, spec := range twitch.ChannelSubscriptions(broadcasterID, w.botID) {
		if err := w.takeSystemHelix(ctx); err != nil {
			return err
		}
		if err := w.twitch.CreateEventSub(ctx, spec, conduitID); err != nil {
			w.conduit.Invalidate()
			return fmt.Errorf("create %s: %w", spec.Type, err)
		}
	}

	return nil
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

// takeGeneralHelix consumes one token from the general Helix budget, the
// partition that backs ordinary api traffic.
func (w *Worker) takeGeneralHelix(ctx context.Context) error {
	return w.take(ctx, ratelimit.Bucket{
		Key:             "ratelimit:helix:app",
		Capacity:        helixGeneralCapacity,
		RefillPerSecond: helixGeneralCapacity / helixWindow,
	})
}

// takeSystemHelix consumes one token from the reserved system partition.
// Only the system lane pays here, so dashboard EventSub jobs always have
// their reserved capacity and can never spend the general api budget.
func (w *Worker) takeSystemHelix(ctx context.Context) error {
	return w.take(ctx, ratelimit.Bucket{
		Key:             "ratelimit:helix:system",
		Capacity:        helixSystemReserve,
		RefillPerSecond: helixSystemReserve / helixWindow,
	})
}

// take consumes one token or returns an error that nacks the message, so the
// paced redelivery retries it once the bucket has refilled.
func (w *Worker) take(ctx context.Context, bucket ratelimit.Bucket) error {

	allowed, err := w.limiter.Allow(ctx, bucket)
	if err != nil {
		return err
	}
	if !allowed {
		return fmt.Errorf("rate limit exceeded on %s", bucket.Key)
	}

	return nil
}

// modStatus resolves whether the bot moderates the channel: a fresh verified
// answer from the registry wins, then live verification when the bot has a
// user token, then whatever the registry holds, then the safe default of
// non-mod, which never over-sends.
func (w *Worker) modStatus(ctx context.Context, payload outgress.Message, ch manage.Channel, found bool) bool {

	if found && !ch.ModCheckedAt.IsZero() && time.Since(ch.ModCheckedAt) < modStatusTTL {
		return ch.IsMod
	}

	botID := w.botID
	if botID == "" {
		botID = payload.SenderID
	}

	isMod, err := w.twitch.IsModerator(ctx, botID, payload.BroadcasterID)
	if err != nil {
		if !errors.Is(err, twitch.ErrNoUserToken) {
			w.log.Warn("mod status verification failed",
				zap.String("broadcaster_id", payload.BroadcasterID),
				zap.Error(err))
		}
		return found && ch.IsMod
	}

	if err := w.registry.SetMod(ctx, payload.BroadcasterID, isMod); err != nil {
		w.log.Warn("failed to cache mod status", zap.Error(err))
	}

	return isMod
}

func (w *Worker) execute(ctx context.Context, payload outgress.Message) error {

	res, err := w.twitch.ExecuteAs(ctx, twitch.ParseIdentity(payload.As),
		payload.BroadcasterID, payload.Method, payload.Endpoint, payload.Payload)
	if err != nil {
		w.log.Error("twitch request failed", zap.Error(err))
		return err
	}
	defer res.Body.Close()

	switch {
	case res.StatusCode == http.StatusTooManyRequests:
		w.log.Warn("twitch rate limited the app",
			zap.String("endpoint", payload.Endpoint),
			zap.Duration("retry_after", twitch.RetryAfter(res)))
		return fmt.Errorf("twitch 429 on %s", payload.Endpoint)

	case res.StatusCode == http.StatusUnauthorized || res.StatusCode == http.StatusForbidden:
		// The client already retried once with a fresh token, so this is a
		// real credentials problem; nack so messages survive until it
		// recovers instead of being silently dropped.
		w.log.Error("twitch rejected our credentials",
			zap.Int("status", res.StatusCode),
			zap.String("endpoint", payload.Endpoint))
		return fmt.Errorf("twitch auth failure: %d", res.StatusCode)

	case res.StatusCode >= 500:
		return fmt.Errorf("twitch server error: %d", res.StatusCode)

	case res.StatusCode >= 400:
		body, _ := io.ReadAll(io.LimitReader(res.Body, 2048))
		w.log.Error("dropping request twitch rejected",
			zap.Int("status", res.StatusCode),
			zap.String("endpoint", payload.Endpoint),
			zap.String("body", string(body)))
		return nil
	}

	return nil
}
