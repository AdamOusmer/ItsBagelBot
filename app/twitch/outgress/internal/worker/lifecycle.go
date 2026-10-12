package worker

import (
	"ItsBagelBot/internal/domain/lifecycle"
	"ItsBagelBot/internal/domain/outgress"
	usersrpc "ItsBagelBot/internal/domain/rpc/users"
	"ItsBagelBot/pkg/bus"
	"ItsBagelBot/pkg/codec"
	"context"
	"errors"
	"strconv"
	"strings"
	"time"
)

const BotModerationSubject = "twitch.ingress.status.authz.bot_moderation"

var errLifecycleStopped = errors.New("channel lifecycle stopped execution")

type admissionFence struct{ incarnation, generation int64 }
type admissionFenceKey struct{}

func (w *Worker) backgroundAllowed(ctx context.Context, id string) bool {
	if w.runtime == nil {
		return true
	}
	uid, err := strconv.ParseUint(id, 10, 64)
	if err != nil {
		return false
	}
	s, err := w.runtime.Get(ctx, uid)
	return err == nil && s.AllowsBackground()
}
func (w *Worker) admitPayload(ctx context.Context, p *outgress.Message) (bool, error) {
	if w.runtime == nil || p.BroadcasterID == "" {
		return true, nil
	}
	// Owner control and recovery reconciliation have their own durable fence.
	if p.Type == outgress.TypeEventSub {
		return true, nil
	}
	uid, err := strconv.ParseUint(p.BroadcasterID, 10, 64)
	if err != nil {
		return false, nil
	}
	s, err := w.runtime.Get(ctx, uid)
	if err != nil {
		return false, err
	}
	if !s.AllowsRuntime() {
		return false, nil
	}
	if p.AccountCreatedAt != 0 && p.AccountCreatedAt != s.AccountCreatedAt {
		return false, nil
	}
	if p.AdmissionGeneration == 0 && s.State.AdmissionGeneration > 1 {
		return false, nil
	}
	if p.AdmissionGeneration != 0 && p.AdmissionGeneration != s.State.AdmissionGeneration {
		return false, nil
	}
	p.AccountCreatedAt = s.AccountCreatedAt
	p.AdmissionGeneration = s.State.AdmissionGeneration
	return true, nil
}
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
	if codec.Unmarshal(msg.Payload, &ev) != nil || ev.UserID != w.botID || ev.BroadcasterID == "" || ev.EventID == "" || ev.ObservedAt.IsZero() {
		return nil
	}
	if ev.Type != "channel.ban" && ev.Type != "channel.unban" {
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
	action := "ban"
	if ev.Type == "channel.unban" {
		action = "unban"
	}
	r := usersrpc.LifecycleRequest{UserID: uid, AccountCreatedAt: current.AccountCreatedAt, AdmissionGeneration: current.State.AdmissionGeneration, Action: action, EventID: ev.EventID, ObservedAt: ev.ObservedAt, Reason: "bot_banned"}
	if !ev.Permanent && !ev.EndsAt.IsZero() {
		r.TimeoutUntil = ev.EndsAt
		r.Action = "timeout"
	}
	changed, err := w.lifecycleRequest(msg.Context(), r)
	if err != nil {
		return err
	}
	if w.runtime != nil {
		if err := w.runtime.Apply(msg.Context(), uid, changed); err != nil {
			return err
		}
	}
	if !changed.Applied {
		return nil
	}
	w.reconcileNotice(msg.Context(), uid, changed)
	if changed.State.ChatRestriction != lifecycle.RestrictionNone {
		return w.registry.SetSubState(msg.Context(), ev.BroadcasterID, subStateBanned, "confirmed bot moderation")
	}
	if changed.State.RuntimeState == lifecycle.Recovering {
		return w.recoverLifecycle(msg.Context(), uid, changed)
	}
	return nil
}
func (w *Worker) recoverLifecycle(ctx context.Context, uid uint64, current usersrpc.LifecycleReply) error {
	r := usersrpc.LifecycleRequest{UserID: uid, AccountCreatedAt: current.AccountCreatedAt, AdmissionGeneration: current.State.AdmissionGeneration, OperationID: current.State.OperationID, Action: "claim"}
	claimed, err := w.lifecycleRequest(ctx, r)
	if err != nil {
		return err
	}
	if w.runtime != nil {
		if err := w.runtime.Apply(ctx, uid, claimed); err != nil {
			return err
		}
	}
	if !claimed.Applied {
		return nil
	}
	id := strconv.FormatUint(uid, 10)
	e := enrollment{broadcasterID: id, incarnation: current.AccountCreatedAt, generation: current.State.AdmissionGeneration, operationID: current.State.OperationID}
	recoveryErr := w.underEnrollLock(ctx, "lifecycle-recovery", e, func() error {
		check, err := w.lifecycleRequest(ctx, usersrpc.LifecycleRequest{UserID: uid, Action: "get"})
		if err != nil {
			return err
		}
		if check.AccountCreatedAt != r.AccountCreatedAt || check.State.AdmissionGeneration != r.AdmissionGeneration || check.State.OperationID != r.OperationID || !check.State.DesiredEnabled {
			return errLifecycleStopped
		}
		if check.State.ChatRestriction != lifecycle.RestrictionNone {
			if err := w.takeSystemHelix(ctx); err != nil {
				return err
			}
			observedAt := time.Now().UTC()
			banned, err := w.twitch.BotBanned(ctx, id, w.botID)
			if err != nil {
				return err
			}
			if banned {
				return errors.New("bot remains banned")
			}
			verifiedReq := r
			verifiedReq.Action = "verified_unban"
			verifiedReq.ObservedAt = observedAt
			verified, err := w.lifecycleRequest(ctx, verifiedReq)
			if err != nil {
				return err
			}
			if !verified.Applied {
				return errLifecycleStopped
			}
			if w.runtime != nil {
				if err := w.runtime.Apply(ctx, uid, verified); err != nil {
					return err
				}
			}
		}

		conduit, err := w.conduit.Get(ctx)
		if err != nil {
			return err
		}
		e.conduitID = conduit
		return w.createAllEventSubs(ctx, e)
	})
	r.Action = "recovery_success"
	if recoveryErr != nil {
		r.Action = "recovery_failed"
		r.Reason = recoveryErr.Error()
	}
	done, err := w.lifecycleRequest(ctx, r)
	if err != nil {
		return err
	}
	if w.runtime != nil {
		if err := w.runtime.Apply(ctx, uid, done); err != nil {
			return err
		}
	}
	if done.Applied && done.State.AllowsBackground() {
		w.recordEnrollSuccess(ctx, e)
		w.seedLiveStatus(ctx, id)
	}
	return nil
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
		_ = w.registry.PruneHot(ctx)
		due, err := w.lifecycleRequest(ctx, usersrpc.LifecycleRequest{Action: "due"})
		if err != nil {
			continue
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
		for _, item := range due.Items {
			if ctx.Err() != nil {
				return
			}
			current := usersrpc.LifecycleReply{AccountCreatedAt: item.AccountCreatedAt, State: item.State}
			if current.State.RuntimeState == lifecycle.Recovering {
				_ = w.recoverLifecycle(ctx, item.UserID, current)
			}
			w.reconcileNotice(ctx, item.UserID, current)
		}
	}
}

func (w *Worker) observeGrant(ctx context.Context, id, action, eventID string, observation ...time.Time) error {
	at := time.Now().UTC()
	if len(observation) > 0 {
		at = observation[0]
		if at.IsZero() || eventID == "" {
			return nil
		}
	}
	uid, err := strconv.ParseUint(id, 10, 64)
	if err != nil {
		return nil
	}
	current, err := w.lifecycleRequest(ctx, usersrpc.LifecycleRequest{UserID: uid, Action: "get"})
	if err != nil {
		return err
	}
	if current.AccountCreatedAt == 0 || at.UnixMicro() < current.AccountCreatedAt {
		return nil
	}
	if fence, ok := ctx.Value(admissionFenceKey{}).(admissionFence); ok && (fence.incarnation != current.AccountCreatedAt || fence.generation != current.State.AdmissionGeneration) {
		return nil
	}
	changed, err := w.lifecycleRequest(ctx, usersrpc.LifecycleRequest{UserID: uid, AccountCreatedAt: current.AccountCreatedAt, AdmissionGeneration: current.State.AdmissionGeneration, Action: action, EventID: eventID, ObservedAt: at, Reason: "authorization_revoked"})
	if err != nil {
		return err
	}
	if w.runtime != nil {
		if err := w.runtime.Apply(ctx, uid, changed); err != nil {
			return err
		}
	}
	w.reconcileNotice(ctx, uid, changed)
	if changed.Applied && changed.State.RuntimeState == lifecycle.Recovering {
		return w.recoverLifecycle(ctx, uid, changed)
	}
	if changed.Applied && action == "grant_restored" && changed.State.DesiredEnabled && changed.State.ChatRestriction != lifecycle.RestrictionNone {
		return w.reconcileControlRepair(ctx, usersrpc.LifecycleWork{UserID: uid, AccountCreatedAt: changed.AccountCreatedAt, State: changed.State})
	}
	return nil
}
func (w *Worker) recordChatOwnership(ctx context.Context, e enrollment) {
	if w.lifecycleNC == nil {
		return
	}
	uid, err := strconv.ParseUint(e.broadcasterID, 10, 64)
	if err != nil {
		return
	}
	current, err := w.lifecycleRequest(ctx, usersrpc.LifecycleRequest{UserID: uid, Action: "get"})
	if err != nil {
		return
	}
	if e.incarnation != 0 && (current.AccountCreatedAt != e.incarnation || current.State.AdmissionGeneration != e.generation) {
		return
	}
	cursor := ""
	ids := []string{}
	chatID := ""
	ban, unban := false, false
	for pages := 0; pages < 10; pages++ {
		if w.takeSystemHelix(ctx) != nil {
			return
		}
		subs, next, err := w.twitch.ListEventSubs(ctx, e.broadcasterID, cursor)
		if err != nil {
			return
		}
		for _, sub := range subs {
			if ownedSub(sub, e) && w.botOwnedSubscription(sub) {
				ids = append(ids, sub.ID)
				switch sub.Type {
				case "channel.chat.message":
					chatID = sub.ID
				case "channel.ban":
					ban = sub.Status == "enabled"
				case "channel.unban":
					unban = sub.Status == "enabled"
				}
			}
		}
		if next == "" {
			break
		}
		cursor = next
		if pages == 9 {
			return
		}
	}
	_ = w.registry.SetIncarnation(ctx, e.broadcasterID, current.AccountCreatedAt)
	healthy := ban && unban
	changed, err := w.lifecycleRequest(ctx, usersrpc.LifecycleRequest{UserID: uid, AccountCreatedAt: current.AccountCreatedAt, AdmissionGeneration: current.State.AdmissionGeneration, Action: "subscription_owned", SubscriptionID: chatID, SubscriptionIDs: ids, ControlListenersHealthy: &healthy})
	if err == nil && changed.Applied && w.runtime != nil {
		_ = w.runtime.Apply(ctx, uid, changed)
	}
}

func (w *Worker) BackgroundAllowed(ctx context.Context, id string) bool {
	return w.backgroundAllowed(ctx, id)
}

func (w *Worker) seedLegacyBlocked(ctx context.Context, item usersrpc.LifecycleWork) {
	id := strconv.FormatUint(item.UserID, 10)
	ch, found, err := w.registry.Get(ctx, id)
	if err != nil {
		return
	}
	if !found {
		changed, err := w.lifecycleRequest(ctx, usersrpc.LifecycleRequest{UserID: item.UserID, AccountCreatedAt: item.AccountCreatedAt, Action: "seed_ack", Reason: "unknown"})
		if err == nil && w.runtime != nil {
			_ = w.runtime.Apply(ctx, item.UserID, changed)
		}
		return
	}
	reason := ""
	if ch.SubState == subStateRevoked || ch.GrantState == "dead" {
		reason = "grant_missing"
	}
	if ch.SubState == subStateBanned {
		reason = "grant_missing"
		if strings.Contains(ch.SubError, "chat_user_banned") {
			reason = "bot_banned"
		}
	}
	if reason == "" {
		_, _ = w.lifecycleRequest(ctx, usersrpc.LifecycleRequest{UserID: item.UserID, AccountCreatedAt: item.AccountCreatedAt, Action: "seed_ack"})
		return
	}
	changed, err := w.lifecycleRequest(ctx, usersrpc.LifecycleRequest{UserID: item.UserID, AccountCreatedAt: item.AccountCreatedAt, AdmissionGeneration: item.State.AdmissionGeneration, Action: "seed_blocked", Reason: reason})
	if err == nil && changed.AccountCreatedAt == item.AccountCreatedAt && (changed.Applied || changed.State.IncidentID != "" || changed.State.AdmissionGeneration > item.State.AdmissionGeneration || !changed.State.GrantObservedAt.IsZero() || !changed.State.ModerationAt.IsZero()) {
		if w.runtime != nil {
			_ = w.runtime.Apply(ctx, item.UserID, changed)
		}
		_, _ = w.lifecycleRequest(ctx, usersrpc.LifecycleRequest{UserID: item.UserID, AccountCreatedAt: item.AccountCreatedAt, Action: "seed_ack"})
	}
}

func (w *Worker) enrollmentAllowed(ctx context.Context, e enrollment) bool {
	if w.runtime == nil {
		return true
	}
	uid, err := strconv.ParseUint(e.broadcasterID, 10, 64)
	if err != nil {
		return false
	}
	s, err := w.runtime.Get(ctx, uid)
	if err != nil || !s.Registered || s.ServiceBanned {
		return false
	}
	if e.incarnation != 0 && e.incarnation != s.AccountCreatedAt {
		return false
	}
	if e.generation != 0 && e.generation != s.State.AdmissionGeneration {
		return false
	}
	if e.operationID != "" {
		return s.State.RuntimeState == lifecycle.Recovering && s.State.OperationID == e.operationID && s.State.DesiredEnabled && s.State.ChatRestriction == lifecycle.RestrictionNone
	}
	return s.AllowsBackground()
}

func (w *Worker) stampCurrent(ctx context.Context, p *outgress.Message) error {
	if w.runtime == nil {
		return nil
	}
	uid, err := strconv.ParseUint(p.BroadcasterID, 10, 64)
	if err != nil {
		return err
	}
	s, err := w.runtime.Get(ctx, uid)
	if err != nil {
		return err
	}
	p.AccountCreatedAt = s.AccountCreatedAt
	p.AdmissionGeneration = s.State.AdmissionGeneration
	return nil
}

func moderationMatchesIncarnation(ev botModeration, current usersrpc.LifecycleReply) bool {
	if current.AccountCreatedAt == 0 || ev.SubscriptionID == "" || ev.ObservedAt.UnixMicro() < current.AccountCreatedAt {
		return false
	}
	for _, id := range current.State.OwnedSubscriptions {
		if id == ev.SubscriptionID {
			return true
		}
	}
	return false
}
