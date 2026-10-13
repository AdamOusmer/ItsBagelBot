package repository

import (
	"ItsBagelBot/app/db/users/ent"
	"ItsBagelBot/app/db/users/ent/user"
	"ItsBagelBot/internal/domain/lifecycle"
	usersrpc "ItsBagelBot/internal/domain/rpc/users"
	"context"
	"time"
)

func (r *Users) lifecycleDue(ctx context.Context) (usersrpc.LifecycleReply, error) {
	now := time.Now().UTC()
	rows, err := r.dueLifecycleRows(ctx, now)
	if err != nil {
		return usersrpc.LifecycleReply{}, err
	}
	reply := usersrpc.LifecycleReply{}
	if err = r.seedLegacyLifecycles(ctx); err != nil {
		return reply, err
	}
	if err = r.pendingLifecycleSeeds(ctx, &reply); err != nil {
		return reply, err
	}
	if err = r.appendLifecycleDue(ctx, &reply, rows, now); err != nil {
		return reply, err
	}
	err = r.erasureDue(ctx, &reply)
	return reply, err
}
func (r *Users) dueLifecycleRows(ctx context.Context, now time.Time) ([]*ent.User, error) {
	return r.client.User.Query().Where(user.Or(user.And(user.LifecycleStateEQ(lifecycle.Active), user.DormantAfterLTE(now)), user.LifecycleDueAtLTE(now), user.NoticeDueAtLTE(now), user.LifecycleProjectionPendingEQ(true))).Order(ent.Asc(user.FieldID)).Limit(100).All(ctx)
}
func (r *Users) seedLegacyLifecycles(ctx context.Context) error {
	rows, err := r.client.User.Query().Where(user.LifecycleStateEQ(""), user.LifecycleIsNil()).Order(ent.Asc(user.FieldID)).Limit(200).All(ctx)
	if err != nil {
		return err
	}
	for _, row := range rows {
		_, err = r.Lifecycle(ctx, usersrpc.LifecycleRequest{UserID: row.ID, AccountCreatedAt: row.CreatedAt.UnixMicro(), Action: "seed"})
		if err != nil {
			return err
		}
	}
	return nil
}
func (r *Users) pendingLifecycleSeeds(ctx context.Context, reply *usersrpc.LifecycleReply) error {
	rows, err := r.client.User.Query().Where(user.LifecycleMigrationPendingEQ(true)).Order(ent.Asc(user.FieldID)).Limit(200).All(ctx)
	if err != nil {
		return err
	}
	for _, row := range rows {
		if row.Lifecycle != nil {
			reply.Seeded = append(reply.Seeded, lifecycleWork(row, *row.Lifecycle))
		}
	}
	return nil
}
func lifecycleWork(row *ent.User, state lifecycle.State) usersrpc.LifecycleWork {
	return usersrpc.LifecycleWork{UserID: row.ID, AccountCreatedAt: row.CreatedAt.UnixMicro(), State: state}
}
func (r *Users) appendLifecycleDue(ctx context.Context, reply *usersrpc.LifecycleReply, rows []*ent.User, now time.Time) error {
	for _, row := range rows {
		if err := r.appendDueAccount(ctx, reply, row, now); err != nil {
			return err
		}
	}
	return nil
}
func (r *Users) appendDueAccount(ctx context.Context, reply *usersrpc.LifecycleReply, row *ent.User, now time.Time) error {
	if err := r.replayLifecycleProjection(ctx, row); err != nil {
		return err
	}
	if row.Lifecycle == nil {
		return nil
	}
	if dormancyDue(row, now) {
		reply.Dormancy = append(reply.Dormancy, lifecycleWork(row, *row.Lifecycle))
	}
	state, err := r.expireDueTimeout(ctx, row)
	if err != nil {
		return err
	}
	appendLifecycleWork(reply, lifecycleWork(row, *state), now)
	return nil
}

func (r *Users) replayLifecycleProjection(ctx context.Context, row *ent.User) error {
	if !row.LifecycleProjectionPending {
		return nil
	}
	return r.publishLifecycleProjection(ctx, row, row.StateRevision)
}
func dormancyDue(row *ent.User, now time.Time) bool {
	if row.Lifecycle.RuntimeState != lifecycle.Active {
		return false
	}
	if row.DormantAfter == nil {
		return false
	}
	return !now.Before(*row.DormantAfter)
}
func (r *Users) expireDueTimeout(ctx context.Context, row *ent.User) (*lifecycle.State, error) {
	if row.Lifecycle.ChatRestriction != lifecycle.RestrictionTimeout {
		return row.Lifecycle, nil
	}
	result, err := r.Lifecycle(ctx, usersrpc.LifecycleRequest{UserID: row.ID, AccountCreatedAt: row.CreatedAt.UnixMicro(), AdmissionGeneration: row.Lifecycle.AdmissionGeneration, Action: "timeout_expired"})
	return &result.State, err
}
func appendLifecycleWork(reply *usersrpc.LifecycleReply, work usersrpc.LifecycleWork, now time.Time) {
	if lifecycleTimeDue(work.State.ControlRepairAt, now) {
		reply.ControlRepair = append(reply.ControlRepair, work)
	}
	if lifecycleTimeDue(work.State.NormalCleanupAt, now) {
		reply.Cleanup = append(reply.Cleanup, work)
	}
	if recoveryDue(work.State, now) || lifecycleTimeDue(work.State.NoticeNextAttemptAt, now) {
		reply.Items = append(reply.Items, work)
	}
}
func recoveryDue(s lifecycle.State, now time.Time) bool {
	if s.RuntimeState == lifecycle.Recovering {
		return true
	}
	return s.RuntimeState == lifecycle.Degraded && lifecycleTimeDue(s.NextAttemptAt, now)
}
func lifecycleTimeDue(at, now time.Time) bool { return !at.IsZero() && !now.Before(at) }
