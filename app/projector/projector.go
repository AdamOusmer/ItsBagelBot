// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package main

import (
	"context"
	"fmt"
	"strconv"

	"ItsBagelBot/app/projector/hydration"
	"ItsBagelBot/internal/domain/event/data"
	"ItsBagelBot/internal/domain/event/twitch"
	"ItsBagelBot/internal/domain/invalidate"
	livekey "ItsBagelBot/internal/domain/live"
	"ItsBagelBot/internal/domain/outgress"
	"ItsBagelBot/internal/domain/validate"
	"ItsBagelBot/internal/projection"
	"ItsBagelBot/pkg/bus"
	"ItsBagelBot/pkg/codec"
	"ItsBagelBot/pkg/monitor"

	"github.com/nats-io/nats.go"
	"go.uber.org/zap"
)

// Projector folds the change events of the data services into the Valkey
// settings projection. Every handler is an overwrite of the new state carried
// by the event itself, which makes redeliveries and full replays safe. The
// message context carries the New Relic transaction opened by the consumer,
// so the store's Valkey segments land on the right trace.
//
// Payloads are validated before they touch Valkey: the bus is internal, but a
// buggy or compromised publisher must not be able to forge projection fields.
// Malformed or invalid events are dropped (logged and acked, never nacked),
// because redelivering a poison message forever helps no one.
type Projector struct {
	store *projection.Store
	nc    *nats.Conn
	// invalidateSubject is a core-NATS (non-queue) subject every projector pod
	// listens on so a user change fans out to all of their in-process tier
	// caches, not just the one durable consumer that folded the event.
	invalidateSubject string
	// cacheInvalidatePrefix is the core-NATS subject prefix used to fan out
	// section-scoped cache invalidations (commands, modules) to the console
	// cache bus after Valkey is updated. Subject = prefix + "." + scope.
	cacheInvalidatePrefix string
	hydrator              *hydration.Hydrator
	// loyalty reads a channel's current lifetime counters to seed the
	// per-stream Overview baseline on go-live (snapshotCounterBaseline). Nil
	// is tolerated (see that method) so tests and any other constructor of
	// Projector need not wire it.
	loyalty loyaltyCounterReader
	log     *zap.Logger
}

// Deps carries the projector's collaborators. invalidateSubject is the
// core-NATS tier-cache fan-out subject; cacheInvalidatePrefix is the
// section-scoped console cache-bus prefix (also used to fan out the go-live
// token-warm, see warmBroadcasterToken).
type Deps struct {
	Store                 *projection.Store
	NC                    *nats.Conn
	InvalidateSubject     string
	CacheInvalidatePrefix string
	Hydrator              *hydration.Hydrator
	Loyalty               loyaltyCounterReader
	Log                   *zap.Logger
}

func NewProjector(d Deps) *Projector {
	return &Projector{
		store:                 d.Store,
		nc:                    d.NC,
		invalidateSubject:     d.InvalidateSubject,
		cacheInvalidatePrefix: d.CacheInvalidatePrefix,
		hydrator:              d.Hydrator,
		loyalty:               d.Loyalty,
		log:                   d.Log,
	}
}

func (p *Projector) HandleUserChanged(msg *bus.Message) error {

	var dto data.UserChangedDTO
	if err := codec.Unmarshal(msg.Payload, &dto); err != nil {
		p.drop(msg, data.SubjectUserChanged, err)
		return nil
	}

	if err := validate.UserID(dto.UserID); err != nil {
		p.drop(msg, data.SubjectUserChanged, err)
		return nil
	}
	if err := validate.Status(dto.Status); err != nil {
		p.drop(msg, data.SubjectUserChanged, err)
		return nil
	}

	if err := p.store.SetUser(msg.Context(), dto.UserID, projection.UserProjection{
		Status:   dto.Status,
		IsActive: dto.IsActive,
		Banned:   dto.Banned,
		Locale:   dto.Locale,
	}); err != nil {
		return err
	}
	p.broadcastInvalidate(dto.UserID)
	return nil
}

func (p *Projector) HandleUserDeleted(msg *bus.Message) error {

	var dto data.UserDeletedDTO
	if err := codec.Unmarshal(msg.Payload, &dto); err != nil {
		p.drop(msg, data.SubjectUserDeleted, err)
		return nil
	}

	if err := validate.UserID(dto.UserID); err != nil {
		p.drop(msg, data.SubjectUserDeleted, err)
		return nil
	}

	if err := p.store.DeleteUser(msg.Context(), dto.UserID); err != nil {
		return err
	}
	p.broadcastInvalidate(dto.UserID)
	return nil
}

// broadcastCacheInvalidate publishes a section-scoped cache invalidation to the
// console cache bus (same subject the users service uses). The optional keys are
// granular identifiers (e.g. a command name and its aliases) so subscribers can
// evict exactly the affected per-command entries instead of a whole section.
// Best effort: Valkey is already written, so a missed ping only delays cache
// staleness until TTL.
func (p *Projector) broadcastCacheInvalidate(userID uint64, scope string, keys ...string) {
	if p.nc == nil || p.cacheInvalidatePrefix == "" {
		return
	}
	if err := invalidate.PublishKeys(p.nc, p.cacheInvalidatePrefix, scope, fmt.Sprint(userID), keys...); err != nil {
		p.log.Warn("failed to broadcast cache invalidation", zap.Uint64("user_id", userID), zap.String("scope", scope), zap.Error(err))
	}
}

// broadcastInvalidate tells every projector pod to drop its in-process tier+ban
// cache for the user. The JetStream user events are folded into Valkey by a
// single pod in the durable group, but the resolved tier/ban decision is cached
// per pod, so the freshly projected state is fanned out over core NATS (no
// queue group) to invalidate all of them. Best effort: Valkey is already
// written, so a missed ping only means a pod serves the prior decision until
// its short TTL lapses.
func (p *Projector) broadcastInvalidate(userID uint64) {
	if p.nc == nil || p.invalidateSubject == "" {
		return
	}
	if err := p.nc.Publish(p.invalidateSubject, []byte(strconv.FormatUint(userID, 10))); err != nil {
		p.log.Warn("failed to broadcast tier cache invalidation", zap.Uint64("user_id", userID), zap.Error(err))
	}
}

func (p *Projector) HandleModuleChanged(msg *bus.Message) error {

	var dto data.ModuleChangedDTO
	if err := codec.Unmarshal(msg.Payload, &dto); err != nil {
		p.drop(msg, data.SubjectModuleChanged, err)
		return nil
	}

	if err := validate.UserID(dto.UserID); err != nil {
		p.drop(msg, data.SubjectModuleChanged, err)
		return nil
	}
	if err := validate.ModuleName(dto.Name); err != nil {
		p.drop(msg, data.SubjectModuleChanged, err)
		return nil
	}
	if err := validate.ConfigsJSON(dto.Configs); err != nil {
		p.drop(msg, data.SubjectModuleChanged, err)
		return nil
	}

	if err := p.store.SetModule(msg.Context(), dto.UserID, projection.ModuleView{
		Name:      dto.Name,
		IsEnabled: dto.IsEnabled,
		Configs:   dto.Configs,
	}); err != nil {
		return err
	}
	p.broadcastCacheInvalidate(dto.UserID, "modules")
	return nil
}

func (p *Projector) HandleCommandChanged(msg *bus.Message) error {
	var dto data.CommandChangedDTO
	if err := codec.Unmarshal(msg.Payload, &dto); err != nil {
		p.drop(msg, data.SubjectCommandChanged, err)
		return nil
	}

	if err := validateCommandChanged(dto); err != nil {
		p.drop(msg, data.SubjectCommandChanged, err)
		return nil
	}

	if err := p.store.SetCommand(msg.Context(), dto); err != nil {
		return err
	}
	// Carry the command name and every alias so each worker evicts exactly the
	// per-command entries that changed, never a whole dictionary.
	keys := append([]string{dto.Name}, dto.Aliases...)
	p.broadcastCacheInvalidate(dto.UserID, "commands", keys...)
	return nil
}

// validateCommandChanged runs every field guard a command-changed event must
// pass. A delete event carries only identity, so the response/perm/cooldown
// fields are validated only on an upsert. The first failing guard's error is
// returned so the caller can drop the event.
func validateCommandChanged(dto data.CommandChangedDTO) error {
	if err := validate.UserID(dto.UserID); err != nil {
		return err
	}
	if err := validate.CommandName(dto.Name); err != nil {
		return err
	}
	if dto.Deleted {
		return nil
	}
	if err := validate.CommandResponse(dto.Response); err != nil {
		return err
	}
	if err := validate.Perm(dto.Perm); err != nil {
		return err
	}
	if err := validate.Cooldown(dto.Cooldown); err != nil {
		return err
	}
	if dto.AllowedUserID != 0 {
		return validate.UserID(dto.AllowedUserID)
	}
	return nil
}

func (p *Projector) drop(msg *bus.Message, subject string, err error) {

	p.log.Warn("dropping invalid event",
		zap.String("subject", subject),
		zap.String("message_id", msg.UUID),
		zap.Error(err),
	)
}

// HandleStreamEvent handles a Twitch EventSub stream-status message off the
// JetStream durable consumer. It decodes the payload via the twitch package
// (which owns the wire shape), persists the live state to Valkey, and triggers
// a full cache refresh when the broadcaster goes live.
//
// It rides a per-service durable consumer (see main), so each subsystem on this
// subject gets its own copy and exactly one pod per group handles each event:
// the refresh fires once, not once per projector pod. Returning an error nacks
// for redelivery; an unparseable payload is dropped (acked) since redelivery
// cannot fix it. A SetStreamLive failure nacks because the live state matters;
// background hydration is best-effort and only logs.
//
// SetStreamLive stays SYNCHRONOUS on purpose. It writes the settings:<id> hash
// "live" field, a DIFFERENT key/namespace from the worker's flat live:<id>
// string, and is NOT on any per-message response path: this is a rare,
// low-frequency stream-event consumer. The synchronous nack-on-failure is a
// deliberate durability property (a dropped live write would silently corrupt
// the projector's GetStreamLive RPC fallback with no redelivery). The per-message
// command latency win lives entirely on the worker side (the node-local replica
// read + the now fire-and-forget greet/live writes), so there is nothing to gain
// by making this async and real correctness to lose. Hydration is already async.
func (p *Projector) HandleStreamEvent(msg *bus.Message) error {
	log := monitor.TxnLogger(msg.Context(), p.log)
	st, ok := twitch.DecodeStreamStatus(msg.Payload)
	if !ok {
		log.Warn("dropping unparseable stream status", zap.String("message_id", msg.UUID))
		return nil
	}

	// Read the prior live state before overwriting it: the Overview's
	// per-stream counter baseline (see snapshotCounterBaseline) must be
	// seeded exactly once per stream, on the false->true edge, and this is
	// the only point that still has "before" in hand. A cold key (never
	// projected) reads back false, which is the correct treatment here too —
	// a channel's first-ever stream event is a go-live like any other.
	//
	// A read failure here defaults to wasLive=true (skip the snapshot this
	// cycle) rather than nacking or defaulting to false: guessing false would
	// risk re-snapshotting an already-live stream mid-run, silently resetting
	// its counters to the current instant, which is strictly worse than the
	// dashboard staying degraded (ok:false) for one stream. SetStreamLive
	// right below keeps its own nack-on-failure — only this baseline
	// decision gets the soft fallback.
	wasLive, _, err := p.store.GetStreamLive(msg.Context(), st.BroadcasterID)
	if err != nil {
		wasLive = true
	}

	if err := p.store.SetStreamLive(msg.Context(), st.BroadcasterID, st.Live); err != nil {
		return err
	}

	// Fan the live change to every live-cache holder (sesame's per-replica bool
	// cache) so a go-live/go-offline is reflected immediately instead of only on
	// the cache's short TTL. Without this the projection is fresh but sesame keeps
	// serving its cached (often stale-offline) answer until the entry lapses. Both
	// directions invalidate: online and offline. Best effort — the projection is
	// already written, so a missed ping only delays visibility until the TTL and
	// the cold-key RPC that reads this same state.
	p.broadcastLiveInvalidate(st.BroadcasterID)

	if !st.Live {
		return nil
	}

	if isGoLiveEdge(wasLive, st.Live) {
		p.snapshotCounterBaseline(msg.Context(), st.BroadcasterID, log)
	}

	log.Info("refreshing settings cache for stream online", zap.Uint64("user_id", st.BroadcasterID))
	p.hydrator.RefreshAsync(st.BroadcasterID)
	p.warmBroadcasterToken(st.BroadcasterID)
	return nil
}

// isGoLiveEdge reports whether a stream-status update is the false->true
// transition the per-stream counter baseline must snapshot on — pulled out
// of HandleStreamEvent as its own pure check so the edge condition itself
// (and not just its Valkey-backed callers) can be unit tested directly.
func isGoLiveEdge(wasLive, isLive bool) bool {
	return isLive && !wasLive
}

// snapshotCounterBaseline seeds the Overview's per-stream counters (see
// internal/projection/valkey.go's StreamCounters/SetStreamCounterBaseline,
// and console/dashboard/src/lib/server/stream-counters.ts which diffs
// against them) with loyalty's current lifetime totals for this channel, the
// instant its stream goes live. HandleStreamEvent calls this only on the
// false->true transition — every other stream event on an already-live
// stream (a redelivery, a later offline) must leave an existing baseline
// untouched, or a resend would silently reset the panel mid-stream.
//
// All three reads must succeed together: a partial baseline (say, a real
// messages count paired with a zero from a failed answered/mod_actions read)
// would understate this stream's totals for the rest of it, and a fully
// zeroed baseline from a loyalty outage would do worse — it would make the
// panel report this channel's entire lifetime total as "this stream" (the
// exact dishonesty stream-counters.ts's header forbids). Skipping the write
// on any failure leaves the baseline absent, which the read side already
// treats as ok:false; the next go-live retries it.
//
// Best-effort like the hydration/token-warm calls beside it in
// HandleStreamEvent: a missed baseline degrades one dashboard panel, not the
// live/offline state HandleStreamEvent's other writes protect with a nack.
func (p *Projector) snapshotCounterBaseline(ctx context.Context, broadcasterID uint64, log *zap.Logger) {
	if p.loyalty == nil {
		return
	}

	uid := strconv.FormatUint(broadcasterID, 10)
	messages, ok1 := p.loyalty.get(ctx, uid, data.CounterMessagesProcessed)
	answered, ok2 := p.loyalty.get(ctx, uid, data.CounterCommandsAnswered)
	modActions, ok3 := p.loyalty.get(ctx, uid, data.CounterModActionsTaken)
	if !ok1 || !ok2 || !ok3 {
		log.Warn("skipping stream counter baseline: loyalty read failed", zap.Uint64("user_id", broadcasterID))
		return
	}

	b := projection.StreamCounters{Messages: messages, Answered: answered, ModActions: modActions}
	if err := p.store.SetStreamCounterBaseline(ctx, uid, b); err != nil {
		log.Warn("failed to write stream counter baseline", zap.Uint64("user_id", broadcasterID), zap.Error(err))
	}
}

// broadcastLiveInvalidate fans a live-state change over the console cache bus on
// the "live" scope sesame's invalidation listener subscribes to, so every sesame
// replica drops its cached live bool and re-reads the freshly projected state.
// Best effort: Valkey is already written, so a missed ping only delays visibility
// until the cache TTL lapses.
func (p *Projector) broadcastLiveInvalidate(userID uint64) {
	if p.nc == nil || p.cacheInvalidatePrefix == "" {
		return
	}
	if err := invalidate.Publish(p.nc, p.cacheInvalidatePrefix, livekey.InvalidateScope, strconv.FormatUint(userID, 10)); err != nil {
		p.log.Warn("failed to broadcast live invalidation", zap.Uint64("user_id", userID), zap.Error(err))
	}
}

// warmBroadcasterToken fans a best-effort request over the core-NATS
// (non-queue) invalidate bus for every outgress replica to pre-mint
// broadcasterID's own Twitch user token into its own in-memory cache, before
// any real automated traffic (chat greetings, mod actions, ads) targets that
// channel.
//
// Numbers (measured 2026-08-20): id.twitch.tv (the OAuth token endpoint) is
// 80.5ms TCP RTT from a cluster pod; a cold TLS handshake plus token request
// costs ~320ms. That gap is almost the entire difference between a channel's
// first automated chat message (~360-390ms) and every one after
// (~15-25ms). outgress mints a broadcaster's own user token lazily, on first
// "as":"broadcaster" use, and each of its 3 replicas keeps an INDEPENDENT
// in-memory token cache (twitch.BroadcasterTokens), so that cold mint can be
// paid up to 3 times per broadcaster, and again after every deploy and every
// LRU eviction. Go-live is the right trigger to pay it ahead of time: it is
// the earliest reliable signal that automated traffic is about to target this
// broadcaster, and it fires once per stream, off the hot path of whichever
// job would otherwise pay the mint first.
//
// FAN-OUT, NOT THE OUTGRESS SYSTEM LANE: an earlier version of this fix
// published a TypeWarmToken job onto the outgress system lane
// (twitch.outgress.system), which is queue-grouped — all 3 replicas share one
// durable consumer group ("outgress-system", see
// app/outgress/main.go's laneSubscribers/startSystemLane) — so exactly one
// replica dequeued the job and only that replica's cache got warmed; the
// other two still paid the cold mint on their own first "as":"broadcaster"
// send. A per-replica in-memory cache needs a per-replica warm, so this
// publishes on the same core-NATS invalidate bus and pattern
// broadcastLiveInvalidate uses above (every replica keeps its own
// subscription, so every replica gets every message), on the dedicated
// outgress.TokenWarmScope. See the outgress worker's SubscribeTokenWarm for
// the receiving side and the concurrent-mint discussion.
//
// Best effort, same convention as broadcastLiveInvalidate above: SetStreamLive
// already wrote the durable live state by the time this runs, so a publish
// failure only costs the pre-fix status quo (a cold mint on first real use).
// This must never nack HandleStreamEvent's message or touch its synchronous
// SetStreamLive durability (see the doc comment on HandleStreamEvent).
func (p *Projector) warmBroadcasterToken(broadcasterID uint64) {
	if p.nc == nil || p.cacheInvalidatePrefix == "" {
		return
	}
	id := strconv.FormatUint(broadcasterID, 10)
	if err := invalidate.Publish(p.nc, p.cacheInvalidatePrefix, outgress.TokenWarmScope, id); err != nil {
		p.log.Warn("failed to publish token-warm fan-out", zap.Uint64("broadcaster_id", broadcasterID), zap.Error(err))
	}
}
