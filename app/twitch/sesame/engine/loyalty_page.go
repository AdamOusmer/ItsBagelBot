// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"

	"ItsBagelBot/app/twitch/sesame/module"
	"ItsBagelBot/internal/domain/event/data"
	"ItsBagelBot/internal/domain/rpc/manage"
	"ItsBagelBot/internal/utils"
	"ItsBagelBot/internal/watchtime"
	"ItsBagelBot/pkg/codec"
	pkg_valkey "ItsBagelBot/pkg/valkey"

	"github.com/nats-io/nuid"
	"go.uber.org/zap"
)

// loyaltyPage binds one bounded unit of work to its tenant, lease and saved
// schedule. Every transition uses that same ownership identity.
type loyaltyPage struct {
	clock *ValkeyLoyaltyClock
	id    uint64
	owner string
	state loyaltySchedule
}

// loyaltyAwardSnapshot keeps the captured account incarnation and its decoded
// earning rules together while a page is validated and saved.
type loyaltyAwardSnapshot struct {
	admission watchtime.Snapshot
	config    LoyaltyModuleConfig
}

func (s *ValkeyLoyaltyClock) fire(parent context.Context, id uint64) {
	ctx, cancel := context.WithTimeout(parent, loyaltyPageTimeout)
	defer cancel()
	page := loyaltyPage{clock: s, id: id, owner: nuid.Next()}
	if !page.claim(ctx) {
		return
	}
	defer page.release()
	state, err := s.readSchedule(ctx, id)
	if err != nil {
		page.retry(ctx, err, 0)
		return
	}
	page.state = state
	started := time.Now()
	defer page.logProcessed(started)
	page.run(ctx)
}

func (p *loyaltyPage) claim(ctx context.Context) bool {
	result, err := p.clock.eval(ctx, loyaltyClaimScript, []string{loyaltyScheduleKey(p.id), loyaltyDueKey, loyaltyClaimKey(p.id)}, strconv.FormatUint(p.id, 10), p.owner, strconv.FormatInt(p.clock.now().UnixMilli(), 10), strconv.FormatInt(loyaltyTickClaimTTL.Milliseconds(), 10))
	if err != nil {
		p.clock.log.Warn("loyalty: window claim failed", module.BIDField(p.id), zap.Error(err))
		return false
	}
	won, err := result.AsInt64()
	return err == nil && won == 1
}

func (p *loyaltyPage) release() {
	ctx, cancel := context.WithTimeout(context.Background(), loyaltyRearmTimeout)
	defer cancel()
	_ = pkg_valkey.NewOwnerLock(p.clock.client, loyaltyClaimKey(p.id), p.owner).Release(ctx)
}

func (p *loyaltyPage) logProcessed(started time.Time) {
	now := p.clock.now().UnixMilli()
	age := max(int64(0), now-p.state.startedAt)
	dueAge := max(int64(0), now-p.state.dueAt)
	p.clock.log.Debug("loyalty: watch page processed", module.BIDField(p.id), zap.String("window_id", p.state.window), zap.Uint32("chunk", p.state.chunk), zap.Int64("collection_age_ms", age), zap.Int64("due_age_ms", dueAge), zap.Int64("overdue_intervals", dueAge/watchTickInterval.Milliseconds()), zap.Duration("processing_duration", time.Since(started)))
}

func (p *loyaltyPage) run(ctx context.Context) {
	snap, allowed := p.capture(ctx)
	if !allowed {
		return
	}
	checkLive := p.state.confirmedAt == 0 || p.clock.now().UnixMilli()-p.state.confirmedAt >= watchTickReconfirmInterval.Milliseconds()
	if !p.readyToFetch(ctx, checkLive) {
		return
	}
	cfg, err := decodeWatchConfiguration(snap.Config)
	if err != nil {
		p.retry(ctx, err, 0)
		return
	}
	reply, err := p.clock.fetchPage(ctx, p.id, p.state, checkLive)
	if err != nil {
		p.retry(ctx, err, 0)
		return
	}
	p.consumeReply(ctx, reply, checkLive, loyaltyAwardSnapshot{admission: snap, config: cfg})
}

func (p *loyaltyPage) capture(ctx context.Context) (watchtime.Snapshot, bool) {
	snap, allowed, err := p.clock.awards.Capture(ctx, p.id)
	if err != nil {
		p.retry(ctx, err, 0)
		return snap, false
	}
	if !allowed {
		p.stop(ctx, "admission changed")
		return snap, false
	}
	if !p.state.matchesAdmission(snap) {
		p.stop(ctx, "admission changed")
		return snap, false
	}
	return snap, true
}
func (state loyaltySchedule) matchesAdmission(snap watchtime.Snapshot) bool {
	if snap.LiveSession == "" {
		return false
	}
	return snap.Generation == state.generation && snap.LiveSession == state.liveSession
}

func (p *loyaltyPage) readyToFetch(ctx context.Context, checkLive bool) bool {
	if p.state.pending != "" {
		if checkLive {
			return true
		}
		p.submitPending(ctx)
		return false
	}
	if p.clock.collectionExpired(p.state) {
		p.abandon(ctx, "collection age exceeded")
		return false
	}
	return true
}
func decodeWatchConfiguration(raw codec.RawMessage) (LoyaltyModuleConfig, error) {
	var cfg LoyaltyModuleConfig
	if len(raw) == 0 {
		return cfg, nil
	}
	if err := codec.Unmarshal(raw, &cfg); err != nil {
		return cfg, fmt.Errorf("invalid loyalty configuration: %w", err)
	}
	return cfg, nil
}

func (p *loyaltyPage) consumeReply(ctx context.Context, reply manage.ChattersReply, checkLive bool, award loyaltyAwardSnapshot) {
	if !p.applyConfirmation(ctx, reply) {
		return
	}
	// Successful reconfirmation releases the immutable saved page even when
	// fetching a later page failed. Fresh viewers never replace that payload.
	if p.state.pending != "" && reply.CheckedAtUnixMilli > 0 {
		p.submitPending(ctx)
		return
	}
	if p.rejectReply(ctx, reply, checkLive) {
		return
	}
	p.warmViewerSnapshot(ctx, reply)
	p.saveReply(ctx, reply, award)
}

func (p *loyaltyPage) applyConfirmation(ctx context.Context, reply manage.ChattersReply) bool {
	if reply.CheckedAtUnixMilli == 0 {
		return true
	}
	if !reply.Live {
		p.stop(ctx, "offline")
		return false
	}
	changed, err := p.confirmStream(ctx, reply)
	return err == nil && !changed
}

func (p *loyaltyPage) rejectReply(ctx context.Context, reply manage.ChattersReply, checkLive bool) bool {
	if reply.Error != "" || reply.MissingScope {
		p.handleReplyFailure(ctx, reply)
		return true
	}
	if checkLive && reply.CheckedAtUnixMilli == 0 {
		p.retry(ctx, errors.New("chatter reply omitted live confirmation"), 0)
		return true
	}
	if reply.Complete {
		return false
	}
	if reply.NextCursor == "" || reply.NextCursor == p.state.cursor {
		p.abandon(ctx, "chatter pagination did not advance")
		return true
	}
	return false
}
func (p *loyaltyPage) handleReplyFailure(ctx context.Context, reply manage.ChattersReply) {
	switch reply.ErrorCode {
	case "inactive":
		p.stop(ctx, "tenant inactive")
	case "invalid", "repeated_cursor":
		p.abandon(ctx, reply.Error)
	default:
		p.retry(ctx, &chattersError{message: reply.Error, missingScope: reply.MissingScope}, reply.RetryAtUnixMilli)
	}
}

func (p *loyaltyPage) warmViewerSnapshot(ctx context.Context, reply manage.ChattersReply) {
	// Only a complete first page represents an authoritative viewer listing.
	if p.state.chunk != 0 {
		return
	}
	if p.state.cursor != "" {
		return
	}
	if !reply.Complete {
		return
	}
	p.clock.viewers.Store(ctx, p.id, viewerSnapshotEntries(reply.Chatters))
}
func (p *loyaltyPage) saveReply(ctx context.Context, reply manage.ChattersReply, award loyaltyAwardSnapshot) {
	dto := p.buildAward(reply, award)
	payload, err := codec.Marshal(dto)
	if err != nil {
		p.retry(ctx, err, 0)
		return
	}
	if !p.saveSnapshot(ctx, reply, payload) {
		return
	}
	p.state.pending = string(payload)
	p.state.nextCursor = reply.NextCursor
	p.state.complete = reply.Complete
	p.submitPending(ctx)
}
func (p *loyaltyPage) buildAward(reply manage.ChattersReply, award loyaltyAwardSnapshot) data.WatchAwardDTO {
	return data.WatchAwardDTO{UserID: p.id, Generation: p.state.generation, LiveSession: p.state.liveSession, WindowID: p.state.window, WindowStartedAtUnixMilli: p.state.startedAt, Chunk: p.state.chunk, AccountCreatedAt: award.admission.AccountCreatedAt, Entries: p.viewerAwards(reply, award.config)}
}
func (p *loyaltyPage) viewerAwards(reply manage.ChattersReply, cfg LoyaltyModuleConfig) []data.LoyaltyEarnEntry {
	entries := make([]data.LoyaltyEarnEntry, 0, len(reply.Chatters))
	seen := map[uint64]struct{}{}
	for _, ch := range reply.Chatters {
		viewer, ok := p.clock.chatterViewerID(ch.ID)
		if !ok {
			continue
		}
		if _, ok := seen[viewer]; ok {
			continue
		}
		seen[viewer] = struct{}{}
		entries = append(entries, data.LoyaltyEarnEntry{ViewerID: viewer, ViewerLogin: ch.Login, Points: cfg.AutomaticPoints(p.id, viewer, cfg.EffectiveWatchPointsPerTick()), WatchSeconds: uint64(watchTickInterval / time.Second)})
	}
	return entries
}
func (p *loyaltyPage) saveSnapshot(ctx context.Context, reply manage.ChattersReply, payload []byte) bool {
	saved, err := p.clock.eval(ctx, loyaltySavePageScript, []string{loyaltyScheduleKey(p.id), loyaltyClaimKey(p.id)}, p.owner, p.state.window, string(payload), reply.NextCursor, utils.BoolField(reply.Complete))
	if err != nil {
		return false
	}
	result, err := saved.AsInt64()
	if err != nil {
		return false
	}
	if result == -1 {
		p.abandon(ctx, "chatter pagination cursor cycle")
	}
	return result == 1
}
