// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package worker

import (
	"context"
	"strings"
	"time"

	"ItsBagelBot/app/twitch/outgress/internal/channels"
	"ItsBagelBot/app/twitch/outgress/internal/twitch"
	"ItsBagelBot/internal/domain/outgress"
	"ItsBagelBot/internal/domain/rpc/manage"

	"go.uber.org/zap"
)

const (
	verifyThrottleTTL         = 30 * time.Second
	repairTimeout             = 60 * time.Second
	subscriptionStatusEnabled = "enabled"
)

func (w *Worker) VerifyChannel(ctx context.Context, broadcasterID string, repair bool) (manage.Verification, error) {
	claimed, err := w.registry.ClaimVerify(ctx, broadcasterID, verifyThrottleTTL)
	if err != nil {
		return manage.Verification{}, err
	}
	if !claimed {
		return manage.Verification{Throttled: true}, nil
	}
	e, err := w.currentEnrollment(ctx, broadcasterID)
	if err != nil {
		return manage.Verification{Error: "channel unavailable"}, nil
	}
	present, err := w.presentSubscriptionTypes(ctx, e)
	if err != nil {
		w.log.Warn("verify: eventsub list failed", zap.String("broadcaster_id", broadcasterID), zap.Error(err))
		return manage.Verification{Error: "twitch list failed"}, nil
	}
	expected := w.requiredSubscriptionTypes(broadcasterID)
	return w.recordVerification(ctx, e, newVerification(expected, present), repair)
}

func (w *Worker) currentEnrollment(ctx context.Context, broadcasterID string) (enrollment, error) {
	conduitID, err := w.conduit.Get(ctx)
	if err != nil {
		return enrollment{}, err
	}
	stamp := outgress.Message{BroadcasterID: broadcasterID}
	if err := w.stampCurrent(ctx, &stamp); err != nil {
		return enrollment{}, err
	}
	return enrollment{broadcasterID: broadcasterID, conduitID: conduitID, incarnation: stamp.AccountCreatedAt, generation: stamp.AdmissionGeneration}, nil
}

func (w *Worker) requiredSubscriptionTypes(broadcasterID string) []string {
	var types []string
	for _, spec := range twitch.ChannelSubscriptions(broadcasterID, w.botID) {
		if !controlSubscription(spec.Type) && w.botOwnedSubscription(entryForSpec(spec, broadcasterID)) {
			types = append(types, spec.Type)
		}
	}
	return types
}

func entryForSpec(spec twitch.SubSpec, broadcasterID string) twitch.EventSubEntry {
	entry := twitch.EventSubEntry{Type: spec.Type}
	entry.Condition.BroadcasterUserID = broadcasterID
	entry.Condition.UserID = spec.Condition["user_id"]
	return entry
}

func (w *Worker) presentSubscriptionTypes(ctx context.Context, e enrollment) (map[string]bool, error) {
	present := map[string]bool{}
	err := w.visitSubscriptionPages(ctx, e, func(sub twitch.EventSubEntry) error {
		if w.countsAsPresent(sub, e) {
			present[sub.Type] = true
		}
		return nil
	})
	return present, err
}

func (w *Worker) countsAsPresent(sub twitch.EventSubEntry, e enrollment) bool {
	return sub.Status == subscriptionStatusEnabled && ownedSub(sub, e) && w.botOwnedSubscription(sub)
}

func newVerification(expected []string, present map[string]bool) manage.Verification {
	v := manage.Verification{Expected: len(expected)}
	for _, kind := range expected {
		if !present[kind] {
			v.Missing = append(v.Missing, kind)
		}
	}
	v.Present = v.Expected - len(v.Missing)
	return v
}

func (w *Worker) recordVerification(ctx context.Context, e enrollment, v manage.Verification, repair bool) (manage.Verification, error) {
	if !w.backgroundAllowed(ctx, e.broadcasterID) {
		return v, nil
	}
	state, errMsg := verifiedSubState(v, repair)
	applied, err := w.registry.SetSubStateUnless(ctx, e.broadcasterID, channels.SubState{State: state, Error: errMsg}, subStateRevoked, subStateBanned)
	if err != nil || !applied {
		return v, err
	}
	if state == subStatePending {
		v.Repairing = true
		w.repairInBackground(ctx, e)
	}
	return v, nil
}

func verifiedSubState(v manage.Verification, repair bool) (string, string) {
	switch {
	case len(v.Missing) == 0:
		return subStateOK, ""
	case repair:
		return subStatePending, ""
	default:
		return subStateFailing, "missing: " + strings.Join(v.Missing, ", ")
	}
}

func (w *Worker) repairInBackground(ctx context.Context, e enrollment) {
	detached, cancel := context.WithTimeout(context.WithoutCancel(ctx), repairTimeout)
	go func() {
		defer cancel()
		if err := w.enableEventSubs(detached, e); err != nil {
			w.log.Warn("verify: repair enrollment failed", zap.String("broadcaster_id", e.broadcasterID), zap.Error(err))
		}
	}()
}
