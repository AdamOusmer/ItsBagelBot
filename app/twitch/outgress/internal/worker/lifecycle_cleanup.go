package worker

import (
	"ItsBagelBot/app/twitch/outgress/internal/twitch"
	"ItsBagelBot/internal/domain/lifecycle"
	usersrpc "ItsBagelBot/internal/domain/rpc/users"
	"context"
	"strconv"
)

// This removes only exact recorded bot subscriptions. Independent streams and
// ban/unban recovery signals survive automatic restriction cleanup.
func (w *Worker) reconcileNormalCleanup(ctx context.Context, item usersrpc.LifecycleWork) {
	req := workRequest(item, "cleanup_claim")
	claimed, err := w.mutateLifecycle(ctx, req)
	if err != nil || !claimed.Applied {
		return
	}
	e := enrollment{broadcasterID: strconv.FormatUint(item.UserID, 10), incarnation: item.AccountCreatedAt, generation: claimed.State.AdmissionGeneration}
	cleanupErr := w.underEnrollLock(ctx, "normal-cleanup", e, func() error { return w.cleanupRestrictedSubscriptions(ctx, e, claimed.State) })
	req.Action = "cleanup_success"
	if cleanupErr != nil {
		req.Action = "cleanup_failed"
	}
	_, _ = w.mutateLifecycle(ctx, req)
}

func (w *Worker) cleanupRestrictedSubscriptions(ctx context.Context, e enrollment, state lifecycle.State) error {
	if !w.enrollmentIdentityMatches(ctx, e) {
		return errLifecycleStopped
	}
	if state.ChatRestriction == lifecycle.RestrictionNone && state.AllowsRuntime() {
		return errLifecycleStopped
	}
	conduit, err := w.conduit.Get(ctx)
	if err != nil {
		return err
	}
	e.conduitID = conduit
	owned := recordedSubscriptionIDs(state.OwnedSubscriptions)
	return w.visitSubscriptionPages(ctx, e, func(sub twitch.EventSubEntry) error {
		if !w.normalCleanupOwned(sub, e, owned) {
			return nil
		}
		if state.DesiredEnabled && controlSubscription(sub.Type) {
			return nil
		}
		return w.deleteFencedSubscription(ctx, e, sub.ID)
	})
}
func (w *Worker) normalCleanupOwned(sub twitch.EventSubEntry, e enrollment, owned map[string]bool) bool {
	if !owned[sub.ID] {
		return false
	}
	if !ownedSub(sub, e) {
		return false
	}
	return w.botOwnedSubscription(sub)
}
func (w *Worker) deleteFencedSubscription(ctx context.Context, e enrollment, id string) error {
	if !w.enrollmentIdentityMatches(ctx, e) {
		return errLifecycleStopped
	}
	if err := w.takeSystemHelix(ctx); err != nil {
		return err
	}
	return w.twitch.DeleteEventSub(ctx, id)
}

func recordedSubscriptionIDs(ids []string) map[string]bool {
	owned := make(map[string]bool, len(ids))
	for _, id := range ids {
		owned[id] = true
	}
	return owned
}
