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
		result, err := bus.RequestJSONTimeout[notificationsrpc.ConnectionReply](ctx, w.lifecycleNC, notificationsrpc.ConnectionSubject+".erase", notificationsrpc.ConnectionRequest{AccountID: e.UserID, AccountCreatedAt: e.AccountCreatedAt, ErasedAt: e.DeletedAt}, 3*time.Second)
		if err == nil && result.Outcome == "erased" {
			req.Action = "erase_ack"
			req.Reason = "notifications"
			_, _ = w.lifecycleRequest(ctx, req)
		}
	}
	if !e.OutgressDone {
		ids := e.State.OwnedSubscriptions
		if len(ids) == 0 && e.State.ChatSubscriptionID != "" {
			ids = []string{e.State.ChatSubscriptionID}
		}
		for _, id := range ids {
			if w.takeSystemHelix(ctx) != nil {
				return
			}
			if w.twitch.DeleteEventSub(ctx, id) != nil {
				return
			}
		}
		// Runtime registry entries are deleted only when the embedded incarnation
		// matches. A newly registered account's hot entry survives this tombstone.
		if err := w.registry.EraseIncarnation(ctx, strconv.FormatUint(e.UserID, 10), e.AccountCreatedAt); err != nil {
			return
		}
		req.Action = "erase_ack"
		req.Reason = "outgress"
		_, _ = w.lifecycleRequest(ctx, req)
	}
}
