package worker

import (
	"context"
	"errors"

	"ItsBagelBot/app/outgress/internal/twitch"
	"ItsBagelBot/internal/domain/outgress"

	"go.uber.org/zap"
)

// processRedemptionUpdate resolves one channel-points redemption (Helix Update
// Redemption Status) as the broadcaster: it marks the redemption FULFILLED or
// CANCELED after sesame ran the reward's action. It pays the general Helix
// budget under the broadcaster's own user bucket. Twitch only allows updating a
// redemption still in the UNFULFILLED state, so a redemption already resolved
// (by a mod, or a skip-queue reward) returns a 4xx that is dropped, not retried.
func (w *Worker) processRedemptionUpdate(ctx context.Context, payload outgress.Message) error {
	if payload.BroadcasterID == "" || payload.RewardID == "" || payload.RedemptionID == "" {
		w.log.Error("dropping redemption update: missing ids",
			zap.String("broadcaster_id", payload.BroadcasterID),
			zap.String("reward_id", payload.RewardID))
		return nil
	}
	status := payload.Status
	if status != outgress.RedemptionFulfilled && status != outgress.RedemptionCanceled {
		w.log.Error("dropping redemption update: bad status",
			zap.String("broadcaster_id", payload.BroadcasterID),
			zap.String("status", status))
		return nil
	}

	payload.As = outgress.AsBroadcaster
	if err := w.takeGeneralHelix(ctx, payload); err != nil {
		return err
	}

	err := w.twitch.UpdateRedemptionStatus(ctx, payload.BroadcasterID, payload.RewardID, payload.RedemptionID, status)
	if err == nil {
		return nil
	}
	if isPermanent(err) || errors.Is(err, twitch.ErrMissingScope) || errors.Is(err, twitch.ErrNoUserToken) {
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
