package worker

import (
	"ItsBagelBot/app/twitch/outgress/internal/twitch"
	"ItsBagelBot/internal/domain/lifecycle"
	usersrpc "ItsBagelBot/internal/domain/rpc/users"
	"context"
	"errors"
	"strconv"
)

func controlSubscription(kind string) bool { return kind == "channel.ban" || kind == "channel.unban" }

// Stream/update/raid subscriptions can also feed independent consumers. They
// are not bot-owned cleanup targets merely because they use our conduit.
func (w *Worker) botOwnedSubscription(sub twitch.EventSubEntry) bool {
	switch sub.Type {
	case "stream.online", "stream.offline", "channel.update", "channel.raid":
		return false
	case twitch.ChatMessageType:
		return sub.Condition.UserID == w.botID
	default:
		for _, spec := range twitch.ChannelSubscriptions(sub.Condition.BroadcasterUserID, w.botID) {
			if spec.Type == sub.Type {
				return true
			}
		}
		for _, spec := range twitch.ChannelOptionalSubscriptions(sub.Condition.BroadcasterUserID) {
			if spec.Type == sub.Type {
				return true
			}
		}
		return false
	}
}
func (w *Worker) enrollmentIdentityMatches(ctx context.Context, e enrollment) bool {
	if w.runtime == nil {
		return true
	}
	if e.incarnation == 0 || e.generation == 0 {
		return false
	}
	uid, err := strconv.ParseUint(e.broadcasterID, 10, 64)
	if err != nil {
		return false
	}
	s, err := w.runtime.Get(ctx, uid)
	return err == nil && s.Registered && s.AccountCreatedAt == e.incarnation && s.State.AdmissionGeneration == e.generation
}
func (w *Worker) deletionIDs(ctx context.Context, e enrollment) ([]string, error) {
	if w.lifecycleNC == nil {
		return w.ownedSubIDs(ctx, e)
	}
	uid, err := strconv.ParseUint(e.broadcasterID, 10, 64)
	if err != nil {
		return nil, err
	}
	s, err := w.lifecycleRequest(ctx, usersrpc.LifecycleRequest{UserID: uid, Action: "get"})
	if err != nil {
		return nil, err
	}
	if s.AccountCreatedAt != e.incarnation || s.State.AdmissionGeneration != e.generation {
		return nil, errLifecycleStopped
	}
	return append([]string(nil), s.State.OwnedSubscriptions...), nil
}
func (w *Worker) recoveryListenersAllowed(ctx context.Context, e enrollment) bool {
	if w.runtime == nil {
		return true
	}
	if !w.enrollmentIdentityMatches(ctx, e) {
		return false
	}
	uid, _ := strconv.ParseUint(e.broadcasterID, 10, 64)
	s, err := w.runtime.Get(ctx, uid)
	return err == nil && !s.ServiceBanned && s.State.DesiredEnabled && s.State.GrantHealth == lifecycle.GrantHealthy
}
func (w *Worker) createControlListeners(ctx context.Context, e enrollment) error {
	if !w.recoveryListenersAllowed(ctx, e) {
		return errLifecycleStopped
	}
	for _, spec := range twitch.ChannelControlSubscriptions(e.broadcasterID) {
		if !w.recoveryListenersAllowed(ctx, e) {
			return errLifecycleStopped
		}
		if err := w.takeSystemHelix(ctx); err != nil {
			w.persistControlHealth(ctx, e, false, "listener_transient")
			return err
		}
		if err := w.twitch.CreateEventSub(ctx, spec, e.conduitID); err != nil {
			reason := "listener_transient"
			if isPermanent(err) {
				reason = "listener_permission"
			}
			w.persistControlHealth(ctx, e, false, reason)
			if isPermanent(err) {
				return nil
			}
			return err
		}
	}
	return nil
}

func (w *Worker) persistControlHealth(ctx context.Context, e enrollment, healthy bool, reason ...string) {
	if w.lifecycleNC == nil {
		return
	}
	uid, err := strconv.ParseUint(e.broadcasterID, 10, 64)
	if err != nil {
		return
	}
	why := ""
	if len(reason) > 0 {
		why = reason[0]
	}
	r, err := w.lifecycleRequest(ctx, usersrpc.LifecycleRequest{Reason: why, UserID: uid, AccountCreatedAt: e.incarnation, AdmissionGeneration: e.generation, Action: "subscription_owned", ControlListenersHealthy: &healthy})
	if err == nil && w.runtime != nil {
		_ = w.runtime.Apply(ctx, uid, r)
	}
}
func (w *Worker) reconcileRecoveryListeners(ctx context.Context, uid uint64, current usersrpc.LifecycleReply) error {
	e := enrollment{broadcasterID: strconv.FormatUint(uid, 10), incarnation: current.AccountCreatedAt, generation: current.State.AdmissionGeneration}
	return w.underEnrollLock(ctx, "recovery-listeners", e, func() error {
		if !w.recoveryListenersAllowed(ctx, e) {
			return nil
		}
		conduit, err := w.conduit.Get(ctx)
		if err != nil {
			return err
		}
		e.conduitID = conduit
		if err := w.createControlListeners(ctx, e); err != nil {
			return err
		}
		w.recordChatOwnership(ctx, e)
		if w.lifecycleNC != nil {
			observed, err := w.lifecycleRequest(ctx, usersrpc.LifecycleRequest{UserID: uid, Action: "get"})
			if err != nil {
				return err
			}
			if !observed.State.ControlListenersHealthy && !observed.State.ControlRepairAt.IsZero() {
				return errors.New("control listeners not yet verified enabled")
			}
		}
		return nil
	})
}

func (w *Worker) reconcileControlRepair(ctx context.Context, item usersrpc.LifecycleWork) error {
	req := usersrpc.LifecycleRequest{UserID: item.UserID, AccountCreatedAt: item.AccountCreatedAt, AdmissionGeneration: item.State.AdmissionGeneration, Action: "control_claim"}
	claimed, err := w.lifecycleRequest(ctx, req)
	if err != nil || !claimed.Applied {
		return err
	}
	if w.runtime != nil {
		if err := w.runtime.Apply(ctx, item.UserID, claimed); err != nil {
			return err
		}
	}
	repairErr := w.reconcileRecoveryListeners(ctx, item.UserID, claimed)
	req.Action = "control_success"
	if repairErr != nil {
		req.Action = "control_failed"
	}
	done, err := w.lifecycleRequest(ctx, req)
	if err != nil {
		return err
	}
	if w.runtime != nil {
		return w.runtime.Apply(ctx, item.UserID, done)
	}
	return nil
}
