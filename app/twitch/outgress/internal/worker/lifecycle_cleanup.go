package worker

import (
	"ItsBagelBot/internal/domain/lifecycle"
	usersrpc "ItsBagelBot/internal/domain/rpc/users"
	"context"
	"strconv"
)

// This removes only exact recorded bot subscriptions. Independent streams and
// ban/unban recovery signals survive automatic restriction cleanup.
func (w *Worker) reconcileNormalCleanup(ctx context.Context, item usersrpc.LifecycleWork) {
	req := usersrpc.LifecycleRequest{UserID: item.UserID, AccountCreatedAt: item.AccountCreatedAt, AdmissionGeneration: item.State.AdmissionGeneration, Action: "cleanup_claim"}
	claimed, err := w.lifecycleRequest(ctx, req)
	if err != nil || !claimed.Applied {
		return
	}
	if w.runtime != nil {
		if w.runtime.Apply(ctx, item.UserID, claimed) != nil {
			return
		}
	}
	e := enrollment{broadcasterID: strconv.FormatUint(item.UserID, 10), incarnation: item.AccountCreatedAt, generation: claimed.State.AdmissionGeneration}
	cleanupErr := w.underEnrollLock(ctx, "normal-cleanup", e, func() error {
		if !w.enrollmentIdentityMatches(ctx, e) {
			return errLifecycleStopped
		}
		if claimed.State.ChatRestriction == lifecycle.RestrictionNone && claimed.State.AllowsRuntime() {
			return errLifecycleStopped
		}
		conduit, err := w.conduit.Get(ctx)
		if err != nil {
			return err
		}
		e.conduitID = conduit
		owned := map[string]bool{}
		for _, id := range claimed.State.OwnedSubscriptions {
			owned[id] = true
		}
		cursor := ""
		for page := 0; page < 10; page++ {
			if !w.enrollmentIdentityMatches(ctx, e) {
				return errLifecycleStopped
			}
			if err := w.takeSystemHelix(ctx); err != nil {
				return err
			}
			subs, next, err := w.twitch.ListEventSubs(ctx, e.broadcasterID, cursor)
			if err != nil {
				return err
			}
			for _, sub := range subs {
				if !owned[sub.ID] || !ownedSub(sub, e) || !w.botOwnedSubscription(sub) || (claimed.State.DesiredEnabled && controlSubscription(sub.Type)) {
					continue
				}
				if !w.enrollmentIdentityMatches(ctx, e) {
					return errLifecycleStopped
				}
				if err := w.takeSystemHelix(ctx); err != nil {
					return err
				}
				if err := w.twitch.DeleteEventSub(ctx, sub.ID); err != nil {
					return err
				}
			}
			if next == "" {
				return nil
			}
			cursor = next
		}
		return errLifecycleStopped
	})
	req.Action = "cleanup_success"
	if cleanupErr != nil {
		req.Action = "cleanup_failed"
	}
	done, err := w.lifecycleRequest(ctx, req)
	if err == nil && w.runtime != nil {
		_ = w.runtime.Apply(ctx, item.UserID, done)
	}
}
