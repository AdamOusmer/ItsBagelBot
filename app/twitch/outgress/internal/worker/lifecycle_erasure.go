package worker

import (
	notificationsrpc "ItsBagelBot/internal/domain/rpc/notifications"
	usersrpc "ItsBagelBot/internal/domain/rpc/users"
	"ItsBagelBot/pkg/bus"
	"context"
	"strconv"
	"time"
)

// Erasure only follows an existing owner/admin account deletion tombstone.
// Exact subscription IDs belong to that old incarnation; no broadcaster-wide
// listing or deletion is allowed after a replacement account can register.
func (w *Worker) reconcileErasure(ctx context.Context, e usersrpc.LifecycleErasure) {
	req := usersrpc.LifecycleRequest{UserID: e.UserID, AccountCreatedAt: e.AccountCreatedAt, Action: "erase_claim"}
	claimed, err := w.lifecycleRequest(ctx, req)
	if err != nil || !claimed.Applied {
		return
	}
	if !e.NotificationsDone {
		w.eraseNotifications(ctx, e)
	}
	if e.OutgressDone {
		return
	}
	if w.eraseOwnedSubscriptions(ctx, e) != nil {
		return
	}
	w.ackErasure(ctx, e, "outgress")
}

func (w *Worker) ackErasure(ctx context.Context, e usersrpc.LifecycleErasure, reason string) {
	_, _ = w.lifecycleRequest(ctx, usersrpc.LifecycleRequest{UserID: e.UserID, AccountCreatedAt: e.AccountCreatedAt, Action: "erase_ack", Reason: reason})
}
func (w *Worker) eraseNotifications(ctx context.Context, e usersrpc.LifecycleErasure) {
	req := notificationsrpc.ConnectionRequest{AccountID: e.UserID, AccountCreatedAt: e.AccountCreatedAt, ErasedAt: e.DeletedAt}
	result, err := bus.RequestJSONTimeout[notificationsrpc.ConnectionReply](ctx, w.lifecycleNC, notificationsrpc.ConnectionSubject+".erase", req, 3*time.Second)
	if err != nil {
		return
	}
	if result.Outcome == "erased" {
		w.ackErasure(ctx, e, "notifications")
	}
}
func (w *Worker) eraseOwnedSubscriptions(ctx context.Context, e usersrpc.LifecycleErasure) error {
	ids := e.State.OwnedSubscriptions
	if len(ids) == 0 && e.State.ChatSubscriptionID != "" {
		ids = []string{e.State.ChatSubscriptionID}
	}
	for _, id := range ids {
		if err := w.takeSystemHelix(ctx); err != nil {
			return err
		}
		if err := w.twitch.DeleteEventSub(ctx, id); err != nil {
			return err
		}
	}
	// Matching incarnation only: a replacement account's registry entry survives.
	return w.registry.EraseIncarnation(ctx, strconv.FormatUint(e.UserID, 10), e.AccountCreatedAt)
}
