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

type Projector struct {
	store                 *projection.Store
	nc                    *nats.Conn
	invalidateSubject     string
	cacheInvalidatePrefix string
	hydrator              *hydration.Hydrator
	loyalty               loyaltyCounterReader
	live                  liveCounterStore
	log                   *zap.Logger
}

type Deps struct {
	Store                 *projection.Store
	NC                    *nats.Conn
	InvalidateSubject     string
	CacheInvalidatePrefix string
	Hydrator              *hydration.Hydrator
	Loyalty               loyaltyCounterReader
	Live                  liveCounterStore
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
		live:                  d.Live,
		log:                   d.Log,
	}
}

func foldEvent[T any](p *Projector, msg *bus.Message, fold eventFold[T]) error {
	var dto T
	if err := codec.Unmarshal(msg.Payload, &dto); err != nil {
		p.drop(msg, fold.subject, err)
		return nil
	}
	if err := fold.validate(dto); err != nil {
		p.drop(msg, fold.subject, err)
		return nil
	}
	return fold.apply(msg.Context(), dto)
}

type eventFold[T any] struct {
	subject  string
	validate func(T) error
	apply    func(context.Context, T) error
}

func (p *Projector) HandleUserChanged(msg *bus.Message) error {
	return foldEvent(p, msg, eventFold[data.UserChangedDTO]{
		subject:  data.SubjectUserChanged,
		validate: validateUserChanged,
		apply:    p.applyUserChanged,
	})
}

func validateUserChanged(dto data.UserChangedDTO) error {
	if err := validate.UserID(dto.UserID); err != nil {
		return err
	}
	return validate.Status(dto.Status)
}

func (p *Projector) applyUserChanged(ctx context.Context, dto data.UserChangedDTO) error {
	if err := p.store.SetUser(ctx, dto.UserID, projection.UserProjection{
		Status:             dto.Status,
		IsActive:           dto.IsActive,
		Banned:             dto.Banned,
		Locale:             dto.Locale,
		CommandsPageHidden: dto.CommandsPageHidden,
	}); err != nil {
		return err
	}
	p.broadcastInvalidate(dto.UserID)
	// After the Valkey write, so sesame's re-read after eviction sees the new state.
	p.broadcastCacheInvalidate(dto.UserID, "status")
	return nil
}

func (p *Projector) HandleUserDeleted(msg *bus.Message) error {
	return foldEvent(p, msg, eventFold[data.UserDeletedDTO]{
		subject:  data.SubjectUserDeleted,
		validate: validateUserDeleted,
		apply:    p.applyUserDeleted,
	})
}

func validateUserDeleted(dto data.UserDeletedDTO) error {
	return validate.UserID(dto.UserID)
}

func (p *Projector) applyUserDeleted(ctx context.Context, dto data.UserDeletedDTO) error {
	if err := p.store.DeleteUser(ctx, dto.UserID); err != nil {
		return err
	}
	if p.live != nil {
		if err := p.live.DeleteLiveCounters(ctx, dto.UserID, boardCounters); err != nil {
			return err
		}
	}
	p.broadcastInvalidate(dto.UserID)
	return nil
}

func (p *Projector) broadcastCacheInvalidate(userID uint64, scope string, keys ...string) {
	if p.nc == nil || p.cacheInvalidatePrefix == "" {
		return
	}
	if err := invalidate.PublishKeys(p.nc, p.cacheInvalidatePrefix, scope, fmt.Sprint(userID), keys...); err != nil {
		p.log.Warn("failed to broadcast cache invalidation", zap.Uint64("user_id", userID), zap.String("scope", scope), zap.Error(err))
	}
}

func (p *Projector) broadcastInvalidate(userID uint64) {
	if p.nc == nil || p.invalidateSubject == "" {
		return
	}
	if err := p.nc.Publish(p.invalidateSubject, []byte(strconv.FormatUint(userID, 10))); err != nil {
		p.log.Warn("failed to broadcast tier cache invalidation", zap.Uint64("user_id", userID), zap.Error(err))
	}
}

func (p *Projector) HandleModuleChanged(msg *bus.Message) error {
	return foldEvent(p, msg, eventFold[data.ModuleChangedDTO]{
		subject:  data.SubjectModuleChanged,
		validate: validateModuleChanged,
		apply:    p.applyModuleChanged,
	})
}

func validateModuleChanged(dto data.ModuleChangedDTO) error {
	if err := validate.UserID(dto.UserID); err != nil {
		return err
	}
	if err := validate.ModuleName(dto.Name); err != nil {
		return err
	}
	return validate.ConfigsJSON(dto.Configs)
}

func (p *Projector) applyModuleChanged(ctx context.Context, dto data.ModuleChangedDTO) error {
	if err := p.store.SetModule(ctx, dto.UserID, projection.ModuleView{
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
	return foldEvent(p, msg, eventFold[data.CommandChangedDTO]{
		subject:  data.SubjectCommandChanged,
		validate: validateCommandChanged,
		apply:    p.applyCommandChanged,
	})
}

func (p *Projector) applyCommandChanged(ctx context.Context, dto data.CommandChangedDTO) error {
	if err := p.store.SetCommand(ctx, dto); err != nil {
		return err
	}
	keys := append([]string{dto.Name}, dto.Aliases...)
	p.broadcastCacheInvalidate(dto.UserID, "commands", keys...)
	return nil
}

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

func (p *Projector) HandleStreamEvent(msg *bus.Message) error {
	log := monitor.TxnLogger(msg.Context(), p.log)
	st, ok := twitch.DecodeStreamStatus(msg.Payload)
	if !ok {
		log.Warn("dropping unparseable stream status", zap.String("message_id", msg.UUID))
		return nil
	}

	// Assume live on a read error: re-baselining a live stream would reset its counters.
	wasLive, _, err := p.store.GetStreamLive(msg.Context(), st.BroadcasterID)
	if err != nil {
		wasLive = true
	}

	if err := p.store.SetStreamLive(msg.Context(), st.BroadcasterID, st.Live); err != nil {
		return err
	}

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

func isGoLiveEdge(wasLive, isLive bool) bool {
	return isLive && !wasLive
}

var baselineCounters = []projection.CounterName{data.CounterMessagesProcessed, data.CounterCommandsAnswered, data.CounterModActionsTaken}

func (p *Projector) snapshotCounterBaseline(ctx context.Context, broadcasterID uint64, log *zap.Logger) {
	vals, ok := p.liveTotals(ctx, broadcasterID, baselineCounters)
	if !ok {
		log.Warn("skipping stream counter baseline: live counters unavailable", zap.Uint64("user_id", broadcasterID))
		return
	}

	b := projection.StreamCounters{
		Messages:   vals[data.CounterMessagesProcessed],
		Answered:   vals[data.CounterCommandsAnswered],
		ModActions: vals[data.CounterModActionsTaken],
	}
	if err := p.store.SetStreamCounterBaseline(ctx, strconv.FormatUint(broadcasterID, 10), b); err != nil {
		log.Warn("failed to write stream counter baseline", zap.Uint64("user_id", broadcasterID), zap.Error(err))
	}
}

func (p *Projector) broadcastLiveInvalidate(userID uint64) {
	if p.nc == nil || p.cacheInvalidatePrefix == "" {
		return
	}
	if err := invalidate.Publish(p.nc, p.cacheInvalidatePrefix, livekey.InvalidateScope, strconv.FormatUint(userID, 10)); err != nil {
		p.log.Warn("failed to broadcast live invalidation", zap.Uint64("user_id", userID), zap.Error(err))
	}
}

func (p *Projector) warmBroadcasterToken(broadcasterID uint64) {
	if p.nc == nil || p.cacheInvalidatePrefix == "" {
		return
	}
	id := strconv.FormatUint(broadcasterID, 10)
	if err := invalidate.Publish(p.nc, p.cacheInvalidatePrefix, outgress.TokenWarmScope, id); err != nil {
		p.log.Warn("failed to publish token-warm fan-out", zap.Uint64("broadcaster_id", broadcasterID), zap.Error(err))
	}
}
