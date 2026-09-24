// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package worker

import (
	"context"
	"errors"
	"fmt"
	"time"

	"ItsBagelBot/app/twitch/outgress/internal/twitch"
	"ItsBagelBot/internal/activity"
	"ItsBagelBot/internal/domain/i18n"
	"ItsBagelBot/internal/domain/outgress"

	"go.uber.org/zap"
)

func (w *Worker) processRedemptionUpdate(ctx context.Context, payload *outgress.Message) error {
	if !w.validRedemption(payload) {
		return nil
	}

	payload.As = outgress.AsBroadcaster
	if err := w.takeGeneralHelix(ctx, payload); err != nil {
		return err
	}

	err := w.twitch.UpdateRedemptionStatus(ctx, payload.BroadcasterID, payload.RewardID, payload.RedemptionID, payload.Status)
	if err == nil {
		activity.Emit(ctx, payload.BroadcasterID, activity.Row{
			Kind: activity.KindReward,
			Text: fmt.Sprintf(i18n.T(payload.Locale, "activity.reward.redemption"), payload.Status),
			Meta: payload.RewardID,
			At:   time.Now(),
		})
		return nil
	}
	if redemptionPermanent(err) {
		w.log.Warn("dropping redemption update: permanent rejection",
			zap.String("broadcaster_id", payload.BroadcasterID),
			zap.String("reward_id", payload.RewardID),
			zap.Error(err))
		noticeError(ctx, err)
		return nil
	}
	w.log.Warn("redemption update failed, will retry",
		zap.String("broadcaster_id", payload.BroadcasterID), zap.Error(err))
	return err
}

func (w *Worker) validRedemption(payload *outgress.Message) bool {
	if missingRedemptionIDs(payload) {
		w.log.Error("dropping redemption update: missing ids",
			zap.String("broadcaster_id", payload.BroadcasterID),
			zap.String("reward_id", payload.RewardID))
		return false
	}
	if !validRedemptionStatus(payload.Status) {
		w.log.Error("dropping redemption update: bad status",
			zap.String("broadcaster_id", payload.BroadcasterID),
			zap.String("status", payload.Status))
		return false
	}
	return true
}

func missingRedemptionIDs(payload *outgress.Message) bool {
	switch "" {
	case payload.BroadcasterID, payload.RewardID, payload.RedemptionID:
		return true
	default:
		return false
	}
}

func validRedemptionStatus(status string) bool {
	return status == outgress.RedemptionFulfilled || status == outgress.RedemptionCanceled
}

func redemptionPermanent(err error) bool {
	return isPermanent(err) || errors.Is(err, twitch.ErrMissingScope) || errors.Is(err, twitch.ErrNoUserToken)
}
