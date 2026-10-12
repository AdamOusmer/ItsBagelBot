package worker

import (
	"ItsBagelBot/internal/domain/lifecycle"
	usersrpc "ItsBagelBot/internal/domain/rpc/users"
	"context"
	"errors"
	"strconv"
	"time"
)

func (w *Worker) recoverLifecycle(ctx context.Context, uid uint64, current usersrpc.LifecycleReply) error {
	req := usersrpc.LifecycleRequest{UserID: uid, AccountCreatedAt: current.AccountCreatedAt, AdmissionGeneration: current.State.AdmissionGeneration, OperationID: current.State.OperationID, Action: "claim"}
	claimed, err := w.mutateLifecycle(ctx, req)
	if err != nil {
		return err
	}
	if !claimed.Applied {
		return nil
	}
	e := enrollment{broadcasterID: strconv.FormatUint(uid, 10), incarnation: req.AccountCreatedAt, generation: req.AdmissionGeneration, operationID: req.OperationID}
	recoveryErr := w.underEnrollLock(ctx, "lifecycle-recovery", e, func() error { return w.enrollRecovery(ctx, e, req) })
	req.Action = "recovery_success"
	if recoveryErr != nil {
		req.Action = "recovery_failed"
		req.Reason = recoveryErr.Error()
	}
	done, err := w.mutateLifecycle(ctx, req)
	if err != nil {
		return err
	}
	if !done.Applied {
		return nil
	}
	if done.State.AllowsBackground() {
		w.recordEnrollSuccess(ctx, e)
		w.seedLiveStatus(ctx, e.broadcasterID)
	}
	return nil
}

func recoveryMatches(current usersrpc.LifecycleReply, req usersrpc.LifecycleRequest) bool {
	if current.AccountCreatedAt != req.AccountCreatedAt {
		return false
	}
	if current.State.AdmissionGeneration != req.AdmissionGeneration {
		return false
	}
	if current.State.OperationID != req.OperationID {
		return false
	}
	return current.State.DesiredEnabled
}
func (w *Worker) enrollRecovery(ctx context.Context, e enrollment, req usersrpc.LifecycleRequest) error {
	current, err := w.lifecycleRequest(ctx, usersrpc.LifecycleRequest{UserID: req.UserID, Action: "get"})
	if err != nil {
		return err
	}
	if !recoveryMatches(current, req) {
		return errLifecycleStopped
	}
	if current.State.ChatRestriction != lifecycle.RestrictionNone {
		if err := w.verifyRecoveryUnban(ctx, e, req); err != nil {
			return err
		}
	}
	conduit, err := w.conduit.Get(ctx)
	if err != nil {
		return err
	}
	e.conduitID = conduit
	return w.createAllEventSubs(ctx, e)
}
func (w *Worker) verifyRecoveryUnban(ctx context.Context, e enrollment, req usersrpc.LifecycleRequest) error {
	if err := w.takeSystemHelix(ctx); err != nil {
		return err
	}
	observedAt := time.Now().UTC()
	banned, err := w.twitch.BotBanned(ctx, e.broadcasterID, w.botID)
	if err != nil {
		return err
	}
	if banned {
		return errors.New("bot remains banned")
	}
	req.Action = "verified_unban"
	req.ObservedAt = observedAt
	verified, err := w.lifecycleRequest(ctx, req)
	if err != nil {
		return err
	}
	if !verified.Applied {
		return errLifecycleStopped
	}
	return w.applyLifecycle(ctx, req.UserID, verified)
}
