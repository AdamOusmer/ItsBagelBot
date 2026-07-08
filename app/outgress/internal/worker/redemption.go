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
	if !w.validRedemption(payload) {
		return nil
	}

	payload.As = outgress.AsBroadcaster
	if err := w.takeGeneralHelix(ctx, payload); err != nil {
		return err
	}

	err := w.twitch.UpdateRedemptionStatus(ctx, payload.BroadcasterID, payload.RewardID, payload.RedemptionID, payload.Status)
	if err == nil {
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

// validRedemption reports whether a redemption job carries the ids and a target
// status Twitch accepts; a malformed job is logged and dropped (returns false).
func (w *Worker) validRedemption(payload outgress.Message) bool {
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

func missingRedemptionIDs(payload outgress.Message) bool {
	return payload.BroadcasterID == "" || payload.RewardID == "" || payload.RedemptionID == ""
}

func validRedemptionStatus(status string) bool {
	return status == outgress.RedemptionFulfilled || status == outgress.RedemptionCanceled
}

// redemptionPermanent reports whether a redemption error can never succeed on
// retry: a permanent Twitch 4xx (e.g. the redemption was already resolved), a
// missing channel-points scope, or no broadcaster token.
func redemptionPermanent(err error) bool {
	return isPermanent(err) || errors.Is(err, twitch.ErrMissingScope) || errors.Is(err, twitch.ErrNoUserToken)
}
