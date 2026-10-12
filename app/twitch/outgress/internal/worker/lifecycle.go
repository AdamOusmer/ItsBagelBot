package worker

import (
	"ItsBagelBot/internal/domain/lifecycle"
	usersrpc "ItsBagelBot/internal/domain/rpc/users"
	"ItsBagelBot/pkg/bus"
	"ItsBagelBot/pkg/codec"
	"context"
	"errors"
	"strconv"
	"time"
)

const BotModerationSubject = "twitch.ingress.status.authz.bot_moderation"

var errLifecycleStopped = errors.New("channel lifecycle stopped execution")

type admissionFence struct{ incarnation, generation int64 }
type admissionFenceKey struct{}

func (w *Worker) lifecycleRequest(ctx context.Context, r usersrpc.LifecycleRequest) (usersrpc.LifecycleReply, error) {
	if w.lifecycleNC == nil {
		return usersrpc.LifecycleReply{}, errors.New("lifecycle authority unavailable")
	}
	return bus.RequestJSONTimeout[usersrpc.LifecycleReply](ctx, w.lifecycleNC, usersrpc.LifecycleSubject, r, 3*time.Second)
}

type botModeration struct {
	Type           string    `json:"type"`
	BroadcasterID  string    `json:"broadcaster_id"`
	UserID         string    `json:"user_id"`
	SubscriptionID string    `json:"subscription_id"`
	EventID        string    `json:"event_id"`
	ObservedAt     time.Time `json:"observed_at"`
	Permanent      bool      `json:"is_permanent"`
	EndsAt         time.Time `json:"ends_at"`
}

func (w *Worker) HandleBotModeration(msg *bus.Message) error {
	var ev botModeration
	if codec.Unmarshal(msg.Payload, &ev) != nil {
		return nil
	}
	if !ev.valid(w.botID) {
		return nil
	}
	uid, err := strconv.ParseUint(ev.BroadcasterID, 10, 64)
	if err != nil {
		return nil
	}
	current, err := w.lifecycleRequest(msg.Context(), usersrpc.LifecycleRequest{UserID: uid, Action: "get"})
	if err != nil {
		return err
	}
	if !moderationMatchesIncarnation(ev, current) {
		return nil
	}
	changed, err := w.mutateLifecycle(msg.Context(), ev.request(uid, current))
	if err != nil {
		return err
	}
	if !changed.Applied {
		return nil
	}
	return w.reconcileModeration(msg.Context(), ev, uid, changed)
}

// ReconcileLifecycle uses Users' indexed due query; it never scans the channel registry.
func (w *Worker) ReconcileLifecycle(ctx context.Context) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
		w.reconcileLifecycleDue(ctx)
	}
}
func (w *Worker) reconcileLifecycleDue(ctx context.Context) {
	_ = w.registry.RenewHot(ctx, w.backgroundAllowed)
	_ = w.registry.PruneHot(ctx)
	due, err := w.lifecycleRequest(ctx, usersrpc.LifecycleRequest{Action: "due"})
	if err != nil {
		return
	}
	for _, item := range due.ControlRepair {
		_ = w.reconcileControlRepair(ctx, item)
	}
	for _, item := range due.Cleanup {
		w.reconcileNormalCleanup(ctx, item)
	}
	for _, item := range due.Dormancy {
		w.reconcileDormancy(ctx, item)
	}
	for _, item := range due.Erasures {
		w.reconcileErasure(ctx, item)
	}
	for _, item := range due.Seeded {
		w.seedLegacyBlocked(ctx, item)
	}
	w.reconcileDueRecovery(ctx, due.Items)
}
func (w *Worker) reconcileDueRecovery(ctx context.Context, items []usersrpc.LifecycleWork) {
	for _, item := range items {
		if ctx.Err() != nil {
			return
		}
		current := usersrpc.LifecycleReply{AccountCreatedAt: item.AccountCreatedAt, State: item.State}
		if current.State.RuntimeState == lifecycle.Degraded {
			current = w.retryDegraded(ctx, item)
		}
		if current.State.RuntimeState == lifecycle.Recovering {
			_ = w.recoverLifecycle(ctx, item.UserID, current)
		}
		w.reconcileNotice(ctx, item.UserID, current)
	}
}

func (w *Worker) retryDegraded(ctx context.Context, item usersrpc.LifecycleWork) usersrpc.LifecycleReply {
	retried, err := w.mutateLifecycle(ctx, workRequest(item, "degraded_retry"))
	if err != nil || !retried.Applied {
		return usersrpc.LifecycleReply{AccountCreatedAt: item.AccountCreatedAt, State: item.State}
	}
	return retried
}

func moderationMatchesIncarnation(ev botModeration, current usersrpc.LifecycleReply) bool {
	if current.AccountCreatedAt == 0 {
		return false
	}
	if ev.SubscriptionID == "" {
		return false
	}
	if ev.ObservedAt.UnixMicro() < current.AccountCreatedAt {
		return false
	}
	for _, id := range current.State.OwnedSubscriptions {
		if id == ev.SubscriptionID {
			return true
		}
	}
	return false
}

// Applying even an unchanged reply repairs a crash between authority commit and projection.
func (w *Worker) applyLifecycle(ctx context.Context, uid uint64, reply usersrpc.LifecycleReply) error {
	if w.runtime == nil {
		return nil
	}
	return w.runtime.Apply(ctx, uid, reply)
}
func (w *Worker) mutateLifecycle(ctx context.Context, req usersrpc.LifecycleRequest) (usersrpc.LifecycleReply, error) {
	reply, err := w.lifecycleRequest(ctx, req)
	if err != nil {
		return reply, err
	}
	return reply, w.applyLifecycle(ctx, req.UserID, reply)
}
func workRequest(item usersrpc.LifecycleWork, action string) usersrpc.LifecycleRequest {
	return usersrpc.LifecycleRequest{UserID: item.UserID, AccountCreatedAt: item.AccountCreatedAt, AdmissionGeneration: item.State.AdmissionGeneration, Action: action}
}

func (ev botModeration) valid(botID string) bool {
	if ev.UserID != botID {
		return false
	}
	if ev.BroadcasterID == "" {
		return false
	}
	if ev.EventID == "" {
		return false
	}
	if ev.ObservedAt.IsZero() {
		return false
	}
	return controlSubscription(ev.Type)
}
func (ev botModeration) request(uid uint64, current usersrpc.LifecycleReply) usersrpc.LifecycleRequest {
	req := usersrpc.LifecycleRequest{UserID: uid, AccountCreatedAt: current.AccountCreatedAt, AdmissionGeneration: current.State.AdmissionGeneration, Action: "ban", EventID: ev.EventID, ObservedAt: ev.ObservedAt, Reason: "bot_banned"}
	if ev.Type == "channel.unban" {
		req.Action = "unban"
	}
	if !ev.Permanent && !ev.EndsAt.IsZero() {
		req.Action = "timeout"
		req.TimeoutUntil = ev.EndsAt
	}
	return req
}
func (w *Worker) reconcileModeration(ctx context.Context, ev botModeration, uid uint64, changed usersrpc.LifecycleReply) error {
	w.reconcileNotice(ctx, uid, changed)
	if changed.State.ChatRestriction != lifecycle.RestrictionNone {
		return w.registry.SetSubState(ctx, ev.BroadcasterID, subStateBanned, "confirmed bot moderation")
	}
	if changed.State.RuntimeState == lifecycle.Recovering {
		return w.recoverLifecycle(ctx, uid, changed)
	}
	return nil
}
