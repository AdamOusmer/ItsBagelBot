// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"context"
	"strconv"

	"ItsBagelBot/app/twitch/sesame/module"
	"ItsBagelBot/internal/domain/event/data"
	"ItsBagelBot/internal/domain/rpc/manage"
	"ItsBagelBot/internal/utils"
	"ItsBagelBot/internal/watchtime"
	"ItsBagelBot/pkg/codec"

	"go.uber.org/zap"
)

type loyaltySchedule struct {
	generation, liveSession, window, cursor, pending, nextCursor string
	chunk                                                        uint32
	confirmedAt, startedAt, dueAt                                int64
	complete                                                     bool
}

func (s *ValkeyLoyaltyClock) readSchedule(ctx context.Context, id uint64) (loyaltySchedule, error) {
	fields, err := s.client.Do(ctx, s.client.B().Hgetall().Key(loyaltyScheduleKey(id)).Build()).AsStrMap()
	if err != nil {
		return loyaltySchedule{}, err
	}
	chunk, err := strconv.ParseUint(fields["chunk"], 10, 32)
	if err != nil {
		return loyaltySchedule{}, err
	}
	confirmed, _ := strconv.ParseInt(fields["confirmed_at"], 10, 64)
	started, _ := strconv.ParseInt(fields["window_started_at"], 10, 64)
	due, _ := strconv.ParseInt(fields["due"], 10, 64)
	return loyaltySchedule{generation: fields["generation"], liveSession: fields["live_session"], window: fields["window"], cursor: fields["cursor"], pending: fields["pending"], nextCursor: fields["pending_cursor"], chunk: uint32(chunk), confirmedAt: confirmed, startedAt: started, dueAt: due, complete: fields["pending_complete"] == "1"}, nil
}

func (s *ValkeyLoyaltyClock) collectionExpired(state loyaltySchedule) bool {
	return state.startedAt == 0 || s.now().UnixMilli()-state.startedAt >= watchCollectionMaxAge.Milliseconds()
}

func (p *loyaltyPage) submitPending(ctx context.Context) {
	var dto data.WatchAwardDTO
	if err := codec.Unmarshal([]byte(p.state.pending), &dto); err != nil {
		p.abandon(ctx, "invalid saved award: "+err.Error())
		return
	}
	if !p.acceptPending(ctx, dto) {
		return
	}
	if !p.state.complete && p.clock.collectionExpired(p.state) {
		p.abandon(ctx, "collection age exceeded after saved page")
		return
	}
	p.commitSavedPage(ctx)
}
func (p *loyaltyPage) acceptPending(ctx context.Context, dto data.WatchAwardDTO) bool {
	if len(dto.Entries) == 0 {
		return true
	}
	if err := watchtime.ValidateAward(dto); err != nil {
		p.abandon(ctx, "invalid saved award: "+err.Error())
		return false
	}
	accepted, err := p.clock.awards.EnqueueOwned(ctx, dto, p.owner)
	if err != nil {
		p.retry(ctx, err, 0)
		return false
	}
	if !accepted {
		p.stop(ctx, "award admission revoked")
		return false
	}
	return true
}
func (p *loyaltyPage) commitSavedPage(ctx context.Context) {
	_, err := p.clock.eval(ctx, loyaltyCommitPageScript, []string{loyaltyScheduleKey(p.id), loyaltyDueKey, loyaltyClaimKey(p.id)}, p.owner, strconv.FormatUint(p.id, 10), p.state.window, p.state.pending, utils.BoolField(p.state.complete), strconv.FormatInt(p.clock.now().UnixMilli(), 10), strconv.FormatInt(watchTickInterval.Milliseconds(), 10))
	if err != nil {
		p.clock.log.Warn("loyalty: cursor commit failed; saved chunk will resume", module.BIDField(p.id), zap.Error(err))
	}
}
func (p *loyaltyPage) stop(ctx context.Context, reason string) {
	_, err := p.clock.eval(ctx, loyaltyStopScript, []string{loyaltyScheduleKey(p.id), loyaltyDueKey, loyaltyClaimKey(p.id)}, p.owner, strconv.FormatUint(p.id, 10), reason, strconv.FormatInt(loyaltyStateRetention.Milliseconds(), 10))
	if err != nil {
		p.clock.log.Warn("loyalty: failed to revoke window", module.BIDField(p.id), zap.Error(err))
	}
}

func (p *loyaltyPage) abandon(ctx context.Context, reason string) {
	_, err := p.clock.eval(ctx, loyaltyAbandonScript, []string{loyaltyScheduleKey(p.id), loyaltyDueKey, loyaltyClaimKey(p.id)}, p.owner, strconv.FormatUint(p.id, 10), strconv.FormatInt(p.clock.now().Add(watchTickInterval).UnixMilli(), 10), reason)
	if err != nil {
		p.clock.log.Warn("loyalty: failed to abandon invalid window", module.BIDField(p.id), zap.Error(err))
	}
}

func (p *loyaltyPage) retry(ctx context.Context, cause error, retryAt int64) {
	now := p.clock.now().UnixMilli()
	if retryAt < now {
		retryAt = now
	}
	result, err := p.clock.eval(ctx, loyaltyRetryScript, []string{loyaltyScheduleKey(p.id), loyaltyDueKey, loyaltyClaimKey(p.id)},
		p.owner, strconv.FormatUint(p.id, 10), strconv.FormatInt(now, 10), strconv.FormatInt(retryAt, 10), cause.Error(), strconv.FormatInt(watchTickQuickRetry.Milliseconds(), 10), strconv.FormatInt(watchTickInterval.Milliseconds(), 10), strconv.Itoa(watchTickQuickRetries))
	if err != nil {
		p.clock.log.Warn("loyalty: retry state unavailable", module.BIDField(p.id), zap.Error(err))
		return
	}
	streak, _ := result.AsInt64()
	if streak <= 0 {
		return
	}
	fields := []zap.Field{module.BIDField(p.id), zap.Int64("consecutive_failures", streak), zap.Error(cause)}
	if streak >= loyaltyEscalationLevel {
		p.clock.log.Error("loyalty: watch window failing", fields...)
	} else {
		p.clock.log.Warn("loyalty: watch window retry scheduled", fields...)
	}
}

func (p *loyaltyPage) confirmStream(ctx context.Context, reply manage.ChattersReply) (bool, error) {
	result, err := p.clock.eval(ctx, loyaltyConfirmScript, []string{loyaltyScheduleKey(p.id), loyaltyClaimKey(p.id), loyaltyDueKey}, p.owner,
		strconv.FormatInt(reply.CheckedAtUnixMilli, 10), reply.StreamID, strconv.FormatInt(reply.StreamStartedAtUnixMilli, 10),
		strconv.FormatUint(p.id, 10), strconv.FormatInt(p.clock.now().Add(watchTickInterval).UnixMilli(), 10))
	if err != nil {
		return false, err
	}
	n, err := result.AsInt64()
	return n != 1, err
}
