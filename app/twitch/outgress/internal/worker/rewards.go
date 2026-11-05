// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package worker

import (
	"context"
	"errors"
	"time"

	"ItsBagelBot/app/twitch/outgress/internal/twitch"
	"ItsBagelBot/internal/domain/outgress"
	"ItsBagelBot/pkg/codec"

	"github.com/google/uuid"
	"go.uber.org/zap"
)

const rewardNotManageable = "reward_not_manageable"

var errRewardSnapshotsUnavailable = errors.New("outgress reward snapshot store unavailable")

func (w *Worker) processRewardUpdate(ctx context.Context, payload *outgress.Message) error {
	var change outgress.RewardUpdate
	if err := codec.Unmarshal(payload.Payload, &change); err != nil || payload.RewardID == "" {
		w.log.Error("dropping reward update: bad message",
			zap.String("broadcaster_id", payload.BroadcasterID),
			zap.String("reward_id", payload.RewardID))
		return nil
	}
	payload.As = outgress.AsBroadcaster
	return w.rewardUpdateResult(ctx, payload, w.applyRewardUpdate(ctx, payload, change))
}

func (w *Worker) applyRewardUpdate(ctx context.Context, payload *outgress.Message, change outgress.RewardUpdate) error {
	if change.ResetOnStreamEnd {
		if err := w.snapshotReward(ctx, payload); err != nil {
			return err
		}
	}
	if err := w.takeGeneralHelix(ctx, payload); err != nil {
		return err
	}
	patch := twitch.RewardPatch{IsEnabled: change.Enabled, Cost: change.Cost}
	return w.twitch.PatchCustomReward(ctx, payload.BroadcasterID, payload.RewardID, patch)
}

// The snapshot is written before the patch, so a later change can never capture a value this run already changed.
func (w *Worker) snapshotReward(ctx context.Context, payload *outgress.Message) error {
	if w.rewardSnapshots == nil {
		return errRewardSnapshotsUnavailable
	}
	taken, err := w.rewardSnapshots.has(ctx, payload.BroadcasterID, payload.RewardID)
	if err != nil || taken {
		return err
	}
	if err := w.takeGeneralHelix(ctx, payload); err != nil {
		return err
	}
	prior, err := w.twitch.GetManageableReward(ctx, payload.BroadcasterID, payload.RewardID)
	if err != nil {
		return err
	}
	return w.rewardSnapshots.saveFirst(ctx, payload.BroadcasterID, payload.RewardID, rewardSnapshot{Enabled: prior.IsEnabled, Cost: prior.Cost})
}

func (w *Worker) rewardUpdateResult(ctx context.Context, payload *outgress.Message, err error) error {
	fields := []zap.Field{
		zap.String("broadcaster_id", payload.BroadcasterID),
		zap.String("reward_id", payload.RewardID),
		zap.Error(err),
	}
	switch {
	case err == nil:
		return nil
	case rewardRefused(err):
		w.log.Warn("dropping reward update: "+rewardNotManageable, fields...)
	case redemptionPermanent(err):
		w.log.Warn("dropping reward update: permanent rejection", fields...)
	default:
		w.log.Warn("reward update failed, will retry", fields...)
		return err
	}
	noticeError(ctx, err)
	return nil
}

func rewardRefused(err error) bool {
	return errors.Is(err, twitch.ErrRewardNotManageable) || errors.Is(err, twitch.ErrRewardNotFound)
}

func (w *Worker) processRewardRestore(ctx context.Context, payload *outgress.Message) error {
	if w.rewardSnapshots == nil {
		return errRewardSnapshotsUnavailable
	}
	payload.As = outgress.AsBroadcaster
	lock := w.rewardSnapshots.restoreLock(payload.BroadcasterID, uuid.NewString())
	won, err := lock.Acquire(ctx, rewardRestoreLockTTL)
	if err != nil || !won {
		return err
	}
	defer func() {
		releaseCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if err := lock.Release(releaseCtx); err != nil {
			w.log.Warn("failed to release reward restore lock", zap.String("broadcaster_id", payload.BroadcasterID), zap.Error(err))
		}
	}()
	return w.restoreRewards(ctx, payload)
}

func (w *Worker) restoreRewards(ctx context.Context, payload *outgress.Message) error {
	saved, err := w.rewardSnapshots.all(ctx, payload.BroadcasterID)
	if err != nil {
		return err
	}
	for rewardID, snapshot := range saved {
		if err := w.restoreReward(ctx, payload, rewardID, snapshot); err != nil {
			return err
		}
	}
	return nil
}

// Only limiter and store errors return here, for redelivery; a Twitch refusal keeps its snapshot for the next stream end without blocking the other rewards.
func (w *Worker) restoreReward(ctx context.Context, payload *outgress.Message, rewardID string, snapshot rewardSnapshot) error {
	if err := w.takeGeneralHelix(ctx, payload); err != nil {
		return err
	}
	patch := twitch.RewardPatch{IsEnabled: &snapshot.Enabled, Cost: &snapshot.Cost}
	err := w.twitch.PatchCustomReward(ctx, payload.BroadcasterID, rewardID, patch)
	if err == nil || errors.Is(err, twitch.ErrRewardNotFound) {
		return w.rewardSnapshots.clear(ctx, payload.BroadcasterID, rewardID)
	}
	w.log.Warn("reward restore failed, keeping snapshot for the next stream end",
		zap.String("broadcaster_id", payload.BroadcasterID),
		zap.String("reward_id", rewardID),
		zap.Error(err))
	noticeError(ctx, err)
	return nil
}
