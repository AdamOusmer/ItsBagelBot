package worker

import (
	"ItsBagelBot/internal/domain/rpc/manage"
	usersrpc "ItsBagelBot/internal/domain/rpc/users"
	"context"
	"strconv"
	"strings"
)

func (w *Worker) seedLegacyBlocked(ctx context.Context, item usersrpc.LifecycleWork) {
	ch, found, err := w.registry.Get(ctx, strconv.FormatUint(item.UserID, 10))
	if err != nil {
		return
	}
	if !found {
		w.ackLegacySeed(ctx, item, "unknown")
		return
	}
	reason := legacyBlockedReason(ch)
	if reason == "" {
		w.ackLegacySeed(ctx, item, "")
		return
	}
	req := workRequest(item, "seed_blocked")
	req.Reason = reason
	changed, err := w.lifecycleRequest(ctx, req)
	if err != nil {
		return
	}
	if !legacySeedResolved(item, changed) {
		return
	}
	_ = w.applyLifecycle(ctx, item.UserID, changed)
	w.ackLegacySeed(ctx, item, "")
}

func legacyBlockedReason(ch manage.Channel) string {
	if !ch.Enabled {
		return ""
	}
	if ch.SubState == subStateBanned {
		if strings.Contains(ch.SubError, "chat_user_banned") {
			return "bot_banned"
		}
		return "grant_missing"
	}
	if ch.SubState == subStateRevoked || ch.GrantState == "dead" {
		return "grant_missing"
	}
	return ""
}
func legacySeedResolved(item usersrpc.LifecycleWork, changed usersrpc.LifecycleReply) bool {
	if changed.AccountCreatedAt != item.AccountCreatedAt {
		return false
	}
	if changed.Applied {
		return true
	}
	if changed.State.IncidentID != "" {
		return true
	}
	if changed.State.AdmissionGeneration > item.State.AdmissionGeneration {
		return true
	}
	if !changed.State.GrantObservedAt.IsZero() {
		return true
	}
	return !changed.State.ModerationAt.IsZero()
}
func (w *Worker) ackLegacySeed(ctx context.Context, item usersrpc.LifecycleWork, reason string) {
	req := usersrpc.LifecycleRequest{UserID: item.UserID, AccountCreatedAt: item.AccountCreatedAt, Action: "seed_ack", Reason: reason}
	if reason == "unknown" {
		_, _ = w.mutateLifecycle(ctx, req)
		return
	}
	_, _ = w.lifecycleRequest(ctx, req)
}
