package repository

import (
	"ItsBagelBot/app/db/users/ent"
	"ItsBagelBot/app/db/users/ent/lifecycleerasure"
	"ItsBagelBot/internal/domain/lifecycle"
	usersrpc "ItsBagelBot/internal/domain/rpc/users"
	"context"
	"errors"
	"time"
)

func (r *Users) erasureDue(ctx context.Context, reply *usersrpc.LifecycleReply) error {
	now := time.Now().UTC()
	rows, err := r.client.LifecycleErasure.Query().Where(lifecycleerasure.CompletedAtIsNil(), lifecycleerasure.NextAttemptAtLTE(now)).Order(ent.Asc(lifecycleerasure.FieldID)).Limit(100).All(ctx)
	if err != nil {
		return err
	}
	for _, row := range rows {
		var state lifecycle.State
		if row.Lifecycle != nil {
			state = *row.Lifecycle
		}
		reply.Erasures = append(reply.Erasures, usersrpc.LifecycleErasure{UserID: row.UserID, AccountCreatedAt: row.AccountCreatedAt, DeletedAt: row.DeletedAt, State: state, NotificationsDone: row.NotificationsDone, OutgressDone: row.OutgressDone})
	}
	return r.pruneLifecycleErasures(ctx, now)
}
func (r *Users) lifecycleErase(ctx context.Context, req usersrpc.LifecycleRequest) (usersrpc.LifecycleReply, error) {
	if req.AccountCreatedAt == 0 {
		return usersrpc.LifecycleReply{}, errors.New("erasure requires incarnation")
	}
	row, err := r.client.LifecycleErasure.Query().Where(lifecycleerasure.UserIDEQ(req.UserID), lifecycleerasure.AccountCreatedAtEQ(req.AccountCreatedAt)).Only(ctx)
	if err != nil {
		return usersrpc.LifecycleReply{}, err
	}
	now := time.Now().UTC()
	reply := usersrpc.LifecycleReply{AccountCreatedAt: row.AccountCreatedAt}
	if row.CompletedAt != nil {
		return reply, nil
	}
	if req.Action == "erase_claim" {
		n, err := r.client.LifecycleErasure.Update().Where(lifecycleerasure.IDEQ(row.ID), lifecycleerasure.NextAttemptAtLTE(now), lifecycleerasure.CompletedAtIsNil()).SetNextAttemptAt(now.Add(time.Minute)).Save(ctx)
		reply.Applied = n == 1
		return reply, err
	}
	return r.ackLifecycleErasure(ctx, row, req.Reason, now)
}

func (r *Users) pruneLifecycleErasures(ctx context.Context, now time.Time) error {
	expired, err := r.client.LifecycleErasure.Query().Where(lifecycleerasure.CompletedAtLT(now.Add(-7 * 24 * time.Hour))).Order(ent.Asc(lifecycleerasure.FieldID)).Limit(100).All(ctx)
	if err != nil {
		return err
	}
	ids := make([]int, 0, len(expired))
	for _, row := range expired {
		ids = append(ids, row.ID)
	}
	if len(ids) > 0 {
		_, err = r.client.LifecycleErasure.Delete().Where(lifecycleerasure.IDIn(ids...)).Exec(ctx)
	}
	return err
}

func (r *Users) ackLifecycleErasure(ctx context.Context, row *ent.LifecycleErasure, participant string, now time.Time) (usersrpc.LifecycleReply, error) {
	reply := usersrpc.LifecycleReply{AccountCreatedAt: row.AccountCreatedAt}
	update := r.client.LifecycleErasure.UpdateOneID(row.ID)
	switch participant {
	case "notifications":
		update.SetNotificationsDone(true)
	case "outgress":
		update.SetOutgressDone(true)
	default:
		return reply, errors.New("unknown erasure participant")
	}
	committed, err := update.Save(ctx)
	if err != nil {
		return reply, err
	}
	if committed.NotificationsDone && committed.OutgressDone {
		err = r.client.LifecycleErasure.UpdateOneID(row.ID).SetCompletedAt(now).Exec(ctx)
	}
	reply.Applied = true
	return reply, err
}
