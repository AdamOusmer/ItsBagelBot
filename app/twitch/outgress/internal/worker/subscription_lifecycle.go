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
	}
	if subscriptionSpecContains(twitch.ChannelSubscriptions(sub.Condition.BroadcasterUserID, w.botID), sub.Type) {
		return true
	}
	return subscriptionSpecContains(twitch.ChannelOptionalSubscriptions(sub.Condition.BroadcasterUserID), sub.Type)
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
	for _, spec := range twitch.ControlSubscriptions(twitch.ChannelSubscriptions(e.broadcasterID, w.botID)) {
		if err := w.createControlListener(ctx, e, spec); err != nil {
			return w.recordControlFailure(ctx, e, err)
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
		return w.verifyControlHealth(ctx, uid)

	})
}

func (w *Worker) reconcileControlRepair(ctx context.Context, item usersrpc.LifecycleWork) error {
	req := workRequest(item, "control_claim")
	claimed, err := w.lifecycleRequest(ctx, req)
	if err != nil || !claimed.Applied {
		return err
	}
	if err := w.applyLifecycle(ctx, item.UserID, claimed); err != nil {
		return err
	}
	repairErr := w.reconcileRecoveryListeners(ctx, item.UserID, claimed)
	req.Action = "control_success"
	if repairErr != nil {
		req.Action = "control_failed"
	}
	_, err = w.mutateLifecycle(ctx, req)
	return err
}

func (w *Worker) recordControlFailure(ctx context.Context, e enrollment, err error) error {
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
func (w *Worker) verifyControlHealth(ctx context.Context, uid uint64) error {
	if w.lifecycleNC == nil {
		return nil
	}
	observed, err := w.lifecycleRequest(ctx, usersrpc.LifecycleRequest{UserID: uid, Action: "get"})
	if err != nil {
		return err
	}
	if observed.State.ControlListenersHealthy {
		return nil
	}
	if observed.State.ControlRepairAt.IsZero() {
		return nil
	}
	return errors.New("control listeners not yet verified enabled")
}

func subscriptionSpecContains(specs []twitch.SubSpec, kind string) bool {
	for _, spec := range specs {
		if spec.Type == kind {
			return true
		}
	}
	return false
}
func (w *Worker) createControlListener(ctx context.Context, e enrollment, spec twitch.SubSpec) error {
	if !w.recoveryListenersAllowed(ctx, e) {
		return errLifecycleStopped
	}
	if err := w.takeSystemHelix(ctx); err != nil {
		return err
	}
	return w.twitch.CreateEventSub(ctx, spec, e.conduitID)
}
