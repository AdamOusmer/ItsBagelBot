package repository

import (
	"ItsBagelBot/app/db/users/ent"
	"ItsBagelBot/app/db/users/ent/user"
	"ItsBagelBot/internal/domain/lifecycle"
	usersrpc "ItsBagelBot/internal/domain/rpc/users"
	"context"
	"time"
)

func (r *Users) commitLifecycle(ctx context.Context, m lifecycleMutation) (bool, error) {
	row, s := m.row, m.state
	defer r.trackAccounts(ctx, m.request.UserID)()
	update := r.client.User.Update().Where(user.IDEQ(m.request.UserID), user.CreatedAtEQ(row.CreatedAt), user.StateRevisionEQ(row.StateRevision)).SetLifecycle(&s).SetLifecycleProjectionPending(true).SetLifecycleState(s.RuntimeState).SetIsActive(lifecycleServing(s, row.Banned))
	setLifecycleMigration(update, m.request.Action)
	setDormancyDeadline(update, s)
	setLifecycleDue(update, s)
	setNoticeDue(update, s)
	n, err := update.Save(ctx)
	return n != 0, err
}
func lifecycleServing(s lifecycle.State, serviceBanned bool) bool {
	if !s.DesiredEnabled {
		return false
	}
	if serviceBanned {
		return false
	}
	if s.ChatRestriction != lifecycle.RestrictionNone {
		return false
	}
	if s.GrantHealth != lifecycle.GrantHealthy {
		return false
	}
	return s.RuntimeState == lifecycle.Active || s.RuntimeState == lifecycle.Dormant
}
func setLifecycleMigration(update *ent.UserUpdate, action string) {
	switch action {
	case "seed":
		update.SetLifecycleMigrationPending(true)
	case "seed_ack":
		update.SetLifecycleMigrationPending(false)
	}
}
func setDormancyDeadline(update *ent.UserUpdate, s lifecycle.State) {
	if s.RuntimeState != lifecycle.Active {
		update.ClearDormantAfter()
		return
	}
	deadline := s.LastEngagedAt.Add(lifecycle.DormancyPeriod)
	if s.DormancyAfter.After(deadline) {
		deadline = s.DormancyAfter
	}
	update.SetDormantAfter(deadline)
}
func setLifecycleDue(update *ent.UserUpdate, s lifecycle.State) {
	dueAt := s.NextAttemptAt
	for _, at := range []time.Time{s.NormalCleanupAt, s.TimeoutUntil, s.ControlRepairAt} {
		if earlierLifecycleDue(at, dueAt) {
			dueAt = at
		}
	}
	if dueAt.IsZero() {
		update.ClearLifecycleDueAt()
	} else {
		update.SetLifecycleDueAt(dueAt)
	}
}
func earlierLifecycleDue(at, current time.Time) bool {
	if at.IsZero() {
		return false
	}
	return current.IsZero() || at.Before(current)
}
func setNoticeDue(update *ent.UserUpdate, s lifecycle.State) {
	if s.NoticeNextAttemptAt.IsZero() {
		update.ClearNoticeDueAt()
	} else {
		update.SetNoticeDueAt(s.NoticeNextAttemptAt)
	}
}
func (r *Users) publishCommittedLifecycle(ctx context.Context, m lifecycleMutation) (usersrpc.LifecycleReply, error) {
	if err := r.publishLifecycleProjection(ctx, m.row, m.row.StateRevision+1); err != nil {
		return usersrpc.LifecycleReply{}, err
	}
	committed, err := r.client.User.Get(ctx, m.request.UserID)
	if err != nil {
		return usersrpc.LifecycleReply{}, err
	}
	return usersrpc.LifecycleReply{State: *committed.Lifecycle, AccountCreatedAt: committed.CreatedAt.UnixMicro(), StateRevision: committed.StateRevision, Applied: true}, nil
}
func (r *Users) publishLifecycleProjection(ctx context.Context, row *ent.User, revision int64) error {
	if err := r.publishChanged(ctx, row.ID); err != nil {
		return err
	}
	if err := r.publishStatusInvalidation(ctx, row.ID); err != nil {
		return err
	}
	_, err := r.client.User.Update().Where(user.IDEQ(row.ID), user.CreatedAtEQ(row.CreatedAt), user.StateRevisionEQ(revision)).SetLifecycleProjectionPending(false).Save(ctx)
	return err
}
