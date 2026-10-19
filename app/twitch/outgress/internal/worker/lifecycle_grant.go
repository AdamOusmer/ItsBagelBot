package worker

import (
	"ItsBagelBot/internal/domain/lifecycle"
	usersrpc "ItsBagelBot/internal/domain/rpc/users"
	"ItsBagelBot/pkg/bus"
	"context"
	"strconv"
	"time"
)

func (w *Worker) observeGrant(ctx context.Context, observation grantObservation) error {
	at, valid := observation.timestamp()
	if !valid {
		return nil
	}
	uid, err := strconv.ParseUint(observation.userID, 10, 64)
	if err != nil {
		return nil
	}
	current, err := w.lifecycleRequest(ctx, usersrpc.LifecycleRequest{UserID: uid, Action: "get"})
	if bus.NotFoundReply(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if !grantObservationMatches(ctx, at, current) {
		return nil
	}
	req := usersrpc.LifecycleRequest{UserID: uid, AccountCreatedAt: current.AccountCreatedAt, AdmissionGeneration: current.State.AdmissionGeneration, Action: observation.action, EventID: observation.eventID, ObservedAt: at, Reason: "authorization_revoked"}
	changed, err := w.mutateLifecycle(ctx, req)
	if err != nil {
		return err
	}
	w.reconcileNotice(ctx, uid, changed)
	if !changed.Applied {
		return nil
	}
	return w.reconcileGrantChange(ctx, uid, observation.action, changed)
}

type grantObservation struct {
	userID, action, eventID string
	observedAt              time.Time
	provider                bool
}

func (o grantObservation) timestamp() (time.Time, bool) {
	if !o.provider {
		return time.Now().UTC(), true
	}
	if o.observedAt.IsZero() {
		return time.Time{}, false
	}
	return o.observedAt, o.eventID != ""
}
func grantObservationMatches(ctx context.Context, at time.Time, current usersrpc.LifecycleReply) bool {
	if current.AccountCreatedAt == 0 {
		return false
	}
	if at.UnixMicro() < current.AccountCreatedAt {
		return false
	}
	fence, ok := ctx.Value(admissionFenceKey{}).(admissionFence)
	if !ok {
		return true
	}
	if fence.incarnation != current.AccountCreatedAt {
		return false
	}
	return fence.generation == current.State.AdmissionGeneration
}
func (w *Worker) reconcileGrantChange(ctx context.Context, uid uint64, action string, changed usersrpc.LifecycleReply) error {
	if changed.State.RuntimeState == lifecycle.Recovering {
		return w.recoverLifecycle(ctx, uid, changed)
	}
	if action != "grant_restored" {
		return nil
	}
	if !changed.State.DesiredEnabled {
		return nil
	}
	if changed.State.ChatRestriction == lifecycle.RestrictionNone {
		return nil
	}
	return w.reconcileControlRepair(ctx, usersrpc.LifecycleWork{UserID: uid, AccountCreatedAt: changed.AccountCreatedAt, State: changed.State})
}
