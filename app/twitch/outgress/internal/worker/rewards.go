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
	pkg_valkey "ItsBagelBot/pkg/valkey"

	"github.com/google/uuid"
	"go.uber.org/zap"
)

const (
	rewardResultWriteTimeout = 2 * time.Second

	errRewardsBusy expectedNackError = "another reward change for this channel is in progress"
)

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
	if w.rewardSnapshots == nil {
		return w.changeReward(ctx, payload, change)
	}
	return w.holdingRewardChanges(ctx, payload.BroadcasterID, func() error {
		return w.changeReward(ctx, payload, change)
	})
}

func (w *Worker) changeReward(ctx context.Context, payload *outgress.Message, change outgress.RewardUpdate) error {
	if change.ResetOnStreamEnd {
		if err := w.stageSnapshot(ctx, payload); err != nil {
			return err
		}
	}
	if err := w.takeGeneralHelix(ctx, payload); err != nil {
		return err
	}
	patch := twitch.RewardPatch{IsEnabled: change.Enabled, Cost: change.Cost}
	err := w.twitch.PatchCustomReward(ctx, payload.BroadcasterID, payload.RewardID, patch)
	if !change.ResetOnStreamEnd {
		return err
	}
	return w.settleSnapshot(ctx, payload, err)
}

// Staged before the patch so a redelivery after an ambiguous failure never reads a value this change already wrote.
func (w *Worker) stageSnapshot(ctx context.Context, payload *outgress.Message) error {
	if w.rewardSnapshots == nil {
		return errRewardSnapshotsUnavailable
	}
	reward := rewardRef{payload.BroadcasterID, payload.RewardID}
	taken, err := w.rewardSnapshots.has(ctx, reward)
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
	return w.rewardSnapshots.stage(ctx, reward, rewardSnapshot{Enabled: prior.IsEnabled, Cost: prior.Cost})
}

// Only a patch that worked owes a restore; a staged value left by a failed one must never overwrite a later edit.
func (w *Worker) settleSnapshot(ctx context.Context, payload *outgress.Message, patchErr error) error {
	reward := rewardRef{payload.BroadcasterID, payload.RewardID}
	switch {
	case patchErr == nil:
		return w.rewardSnapshots.commit(ctx, reward)
	case rewardRefused(patchErr), redemptionPermanent(patchErr):
		if err := w.rewardSnapshots.discard(ctx, reward); err != nil {
			w.log.Warn("failed to discard staged reward snapshot", zap.String("reward_id", payload.RewardID), zap.Error(err))
		}
	}
	return patchErr
}

func (w *Worker) holdingRewardChanges(ctx context.Context, broadcasterID string, do func() error) error {
	lock := w.rewardSnapshots.changeLock(broadcasterID, uuid.NewString())
	if err := awaitRewardLock(ctx, lock); err != nil {
		return err
	}
	defer w.releaseRewardLock(broadcasterID, lock)
	return do()
}

func awaitRewardLock(ctx context.Context, lock pkg_valkey.OwnerLock) error {
	deadline := time.Now().Add(rewardChangeLockWait)
	for {
		won, err := lock.Acquire(ctx, rewardRestoreLockTTL)
		if err != nil || won {
			return err
		}
		if time.Now().After(deadline) {
			return errRewardsBusy
		}
		if err := sleepRewardLockPoll(ctx); err != nil {
			return err
		}
	}
}

func sleepRewardLockPoll(ctx context.Context) error {
	timer := time.NewTimer(rewardChangeLockPoll)
	defer timer.Stop()
	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (w *Worker) releaseRewardLock(broadcasterID string, lock pkg_valkey.OwnerLock) {
	releaseCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := lock.Release(releaseCtx); err != nil {
		w.log.Warn("failed to release reward lock", zap.String("broadcaster_id", broadcasterID), zap.Error(err))
	}
}

func (w *Worker) rewardUpdateResult(ctx context.Context, payload *outgress.Message, err error) error {
	fields := []zap.Field{
		zap.String("broadcaster_id", payload.BroadcasterID),
		zap.String("reward_id", payload.RewardID),
		zap.Error(err),
	}
	switch {
	case err == nil:
		w.recordRewardResult(ctx, payload, outgress.RewardResultOK)
		return nil
	case rewardRefused(err):
		w.log.Warn("dropping reward update: "+outgress.RewardResultNotManageable, fields...)
		w.recordRewardResult(ctx, payload, rewardRefusalResult(err))
	case redemptionPermanent(err):
		w.log.Warn("dropping reward update: permanent rejection", fields...)
		w.recordRewardResult(ctx, payload, outgress.RewardResultRejected)
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

func rewardRefusalResult(err error) string {
	if errors.Is(err, twitch.ErrRewardNotFound) {
		return outgress.RewardResultNotFound
	}
	return outgress.RewardResultNotManageable
}

func namesARunStep(payload *outgress.Message) bool {
	return payload.RunID != "" && payload.ActionSlot != ""
}

func (w *Worker) recordRewardResult(ctx context.Context, payload *outgress.Message, result string) {
	if w.rewardSnapshots == nil || !namesARunStep(payload) {
		return
	}
	writeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), rewardResultWriteTimeout)
	defer cancel()
	if err := w.rewardSnapshots.recordResult(writeCtx, payload, result); err != nil {
		w.log.Warn("failed to record reward result", zap.String("run_id", payload.RunID), zap.Error(err))
	}
}

// Covers every way a delivery can run out: Twitch errors, a held lock, a pause, and batch items never reached.
func (w *Worker) recordExhaustedRewards(ctx context.Context, payload *outgress.Message) {
	if payload.Type != outgress.TypeBatch {
		w.recordExhaustedReward(ctx, payload, payload.BroadcasterID)
		return
	}
	var batch outgress.Batch
	if decodeBatch(payload.Payload, &batch) != nil {
		return
	}
	for i := range batch.Items {
		w.recordExhaustedReward(ctx, &batch.Items[i], payload.BroadcasterID)
	}
}

func (w *Worker) recordExhaustedReward(ctx context.Context, item *outgress.Message, broadcasterID string) {
	if item.Type != outgress.TypeRewardUpdate {
		return
	}
	if item.BroadcasterID == "" {
		item.BroadcasterID = broadcasterID
	}
	w.recordRewardResult(ctx, item, outgress.RewardResultUnconfirmed)
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
	defer w.releaseRewardLock(payload.BroadcasterID, lock)
	return w.holdingRewardChanges(ctx, payload.BroadcasterID, func() error {
		return w.restoreRewards(ctx, payload)
	})
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
		return w.rewardSnapshots.clear(ctx, rewardRef{payload.BroadcasterID, rewardID})
	}
	w.log.Warn("reward restore failed, keeping snapshot for the next stream end",
		zap.String("broadcaster_id", payload.BroadcasterID),
		zap.String("reward_id", rewardID),
		zap.Error(err))
	noticeError(ctx, err)
	return nil
}
