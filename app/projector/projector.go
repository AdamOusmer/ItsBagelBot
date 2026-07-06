package main

import (
	"encoding/json"
	"fmt"
	"strconv"

	"ItsBagelBot/app/projector/hydration"
	"ItsBagelBot/internal/domain/event/data"
	"ItsBagelBot/internal/domain/event/twitch"
	"ItsBagelBot/internal/domain/invalidate"
	livekey "ItsBagelBot/internal/domain/live"
	"ItsBagelBot/internal/domain/validate"
	"ItsBagelBot/internal/projection"

	"github.com/ThreeDotsLabs/watermill/message"
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
	log                   *zap.Logger
}

func NewProjector(store *projection.Store, nc *nats.Conn, invalidateSubject string, cacheInvalidatePrefix string, hydrator *hydration.Hydrator, log *zap.Logger) *Projector {
	return &Projector{
		store:                 store,
		nc:                    nc,
		invalidateSubject:     invalidateSubject,
		cacheInvalidatePrefix: cacheInvalidatePrefix,
		hydrator:              hydrator,
		log:                   log,
	}
}

func (p *Projector) HandleUserChanged(msg *message.Message) error {

	var dto data.UserChangedDTO
	if err := json.Unmarshal(msg.Payload, &dto); err != nil {
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

	if err := p.store.SetUser(msg.Context(), dto.UserID, dto.Status, dto.IsActive, dto.Banned, dto.Locale); err != nil {
		return err
	}
	p.broadcastInvalidate(dto.UserID)
	return nil
}

func (p *Projector) HandleUserDeleted(msg *message.Message) error {

	var dto data.UserDeletedDTO
	if err := json.Unmarshal(msg.Payload, &dto); err != nil {
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

func (p *Projector) HandleModuleChanged(msg *message.Message) error {

	var dto data.ModuleChangedDTO
	if err := json.Unmarshal(msg.Payload, &dto); err != nil {
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

	if err := p.store.SetModule(msg.Context(), dto.UserID, dto.Name, dto.IsEnabled, dto.Configs); err != nil {
		return err
	}
	p.broadcastCacheInvalidate(dto.UserID, "modules")
	return nil
}

func (p *Projector) HandleCommandChanged(msg *message.Message) error {
	var dto data.CommandChangedDTO
	if err := json.Unmarshal(msg.Payload, &dto); err != nil {
		p.drop(msg, data.SubjectCommandChanged, err)
		return nil
	}

	if err := validate.UserID(dto.UserID); err != nil {
		p.drop(msg, data.SubjectCommandChanged, err)
		return nil
	}
	if err := validate.CommandName(dto.Name); err != nil {
		p.drop(msg, data.SubjectCommandChanged, err)
		return nil
	}
	if !dto.Deleted {
		if err := validate.CommandResponse(dto.Response); err != nil {
			p.drop(msg, data.SubjectCommandChanged, err)
			return nil
		}
		if err := validate.Perm(dto.Perm); err != nil {
			p.drop(msg, data.SubjectCommandChanged, err)
			return nil
		}
		if err := validate.Cooldown(dto.Cooldown); err != nil {
			p.drop(msg, data.SubjectCommandChanged, err)
			return nil
		}
		if dto.AllowedUserID != 0 {
			if err := validate.UserID(dto.AllowedUserID); err != nil {
				p.drop(msg, data.SubjectCommandChanged, err)
				return nil
			}
		}
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

func (p *Projector) drop(msg *message.Message, subject string, err error) {

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
func (p *Projector) HandleStreamEvent(msg *message.Message) error {
	st, ok := twitch.DecodeStreamStatus(msg.Payload)
	if !ok {
		p.log.Warn("dropping unparseable stream status", zap.String("message_id", msg.UUID))
		return nil
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

	p.log.Info("refreshing settings cache for stream online", zap.Uint64("user_id", st.BroadcasterID))
	p.hydrator.RefreshAsync(st.BroadcasterID)
	return nil
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
