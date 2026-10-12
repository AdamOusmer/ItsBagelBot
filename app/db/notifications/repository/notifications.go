// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository

import (
	"ItsBagelBot/app/db/notifications/ent/connectionguard"
	"context"
	"fmt"
	"time"

	"ItsBagelBot/app/db/notifications/ent"
	"ItsBagelBot/app/db/notifications/ent/notification"
	"ItsBagelBot/app/db/notifications/ent/notificationread"
	"ItsBagelBot/app/db/notifications/ent/predicate"
	"ItsBagelBot/pkg/db"
)

const (
	AdminPageSize = 20
	AdminMaxPages = 25
	UserListLimit = 50
)

type Notifications struct {
	client *ent.Client
}

func New(client *ent.Client) *Notifications {
	return &Notifications{client: client}
}

type CreateParams struct {
	Incarnation    string
	RequestID      string
	Scope          notification.Scope
	TargetUserID   *uint64
	Title          string
	Body           string
	Level          notification.Level
	CreatedBy      uint64
	CreatedByLogin string
	ExpiresAt      *time.Time
}

func (r *Notifications) Create(ctx context.Context, p CreateParams) (*ent.Notification, bool, error) {
	client := r.client
	var tx *ent.Tx
	if p.Incarnation != "" && p.TargetUserID != nil {
		var err error
		tx, err = r.client.Tx(ctx)
		if err != nil {
			return nil, false, err
		}
		defer tx.Rollback()
		client = tx.Client()
		if err = lockNotificationIncarnation(ctx, tx, *p.TargetUserID, p.Incarnation); err != nil {
			return nil, false, err
		}
	}
	q := client.Notification.Create().SetScope(p.Scope).SetTitle(p.Title).SetBody(p.Body).SetLevel(p.Level).SetCreatedBy(p.CreatedBy).SetCreatedByLogin(p.CreatedByLogin).SetNillableTargetUserID(p.TargetUserID).SetNillableExpiresAt(p.ExpiresAt).SetIncarnation(p.Incarnation)
	if p.RequestID != "" {
		q.SetRequestID(p.RequestID)
	}
	row, err := q.Save(ctx)
	if err == nil {
		if tx != nil {
			err = tx.Commit()
		}
		return row, true, err
	}
	if !ent.IsConstraintError(err) || p.RequestID == "" {
		return nil, false, err
	}
	row, err = client.Notification.Query().Where(notification.RequestIDEQ(p.RequestID)).Only(ctx)
	if err == nil && tx != nil {
		err = tx.Commit()
	}
	return row, false, err
}

// Both acknowledgement and direct-send transactions serialize against erasure.
func lockNotificationIncarnation(ctx context.Context, tx *ent.Tx, id uint64, incarnation string) error {
	if err := tx.ConnectionGuard.Create().SetAccountID(id).SetIncarnation(incarnation).SetRetainUntil(time.Now().Add(8*24*time.Hour)).OnConflictColumns(connectionguard.FieldAccountID, connectionguard.FieldIncarnation).Update(func(u *ent.ConnectionGuardUpsert) { u.SetAccountID(id) }).Exec(ctx); err != nil {
		return err
	}
	guard, err := tx.ConnectionGuard.Query().Where(connectionguard.AccountIDEQ(id), connectionguard.IncarnationEQ(incarnation)).Only(ctx)
	if err != nil {
		return err
	}
	if guard.Erased {
		return fmt.Errorf("account incarnation erased")
	}
	return nil
}

func (r *Notifications) ListForAdmin(ctx context.Context, limit, offset int) ([]*ent.Notification, error) {
	return db.WithQuery(ctx, func(ctx context.Context) ([]*ent.Notification, error) {
		return r.client.Notification.Query().
			Order(ent.Desc(notification.FieldCreatedAt)).
			Offset(offset).
			Limit(limit).
			All(ctx)
	})
}

func (r *Notifications) CountForAdmin(ctx context.Context) (int, error) {
	return db.WithQuery(ctx, func(ctx context.Context) (int, error) {
		return r.client.Notification.Query().Count(ctx)
	})
}

func visibleForUser(userID uint64, now time.Time) predicate.Notification {
	return notification.And(
		notification.Or(
			notification.ScopeEQ(notification.ScopeBroadcast),
			notification.TargetUserIDEQ(userID),
		),
		notification.Or(
			notification.ExpiresAtIsNil(),
			notification.ExpiresAtGT(now),
		),
	)
}

func lapsedForUser(userID uint64, now time.Time) predicate.Notification {
	return notification.HasReadsWith(
		notificationread.UserIDEQ(userID),
		notificationread.ExpiresAtNotNil(),
		notificationread.ExpiresAtLTE(now),
	)
}

func (r *Notifications) ListForUser(ctx context.Context, userID uint64, limit int) ([]*ent.Notification, map[int]bool, error) {
	return r.ListForIncarnation(ctx, userID, "", time.Time{}, limit)
}
func (r *Notifications) ListForIncarnation(ctx context.Context, userID uint64, incarnation string, createdAt time.Time, limit int) ([]*ent.Notification, map[int]bool, error) {
	now := time.Now()

	rows, err := db.WithQuery(ctx, func(ctx context.Context) ([]*ent.Notification, error) {
		return r.client.Notification.Query().
			Where(
				visibleForUser(userID, now),
				notification.Or(notification.ScopeEQ(notification.ScopeBroadcast), notification.IncarnationEQ(incarnation), notification.And(notification.Or(notification.IncarnationEQ(""), notification.IncarnationIsNil()), notification.CreatedAtGTE(createdAt))),
				notification.Not(lapsedForIncarnation(userID, incarnation, createdAt, now)),
			).
			Order(ent.Desc(notification.FieldCreatedAt)).
			Limit(limit).
			All(ctx)
	})
	if err != nil {
		return nil, nil, err
	}
	if len(rows) == 0 {
		return rows, map[int]bool{}, nil
	}

	ids := make([]int, len(rows))
	for i, row := range rows {
		ids[i] = row.ID
	}

	readRows, err := db.WithQuery(ctx, func(ctx context.Context) ([]*ent.NotificationRead, error) {
		return r.client.NotificationRead.Query().
			Where(
				notificationread.UserIDEQ(userID),
				receiptIncarnation(incarnation, createdAt),
				notificationread.HasNotificationWith(notification.IDIn(ids...)),
			).
			WithNotification().
			All(ctx)
	})
	if err != nil {
		return nil, nil, err
	}

	read := make(map[int]bool, len(readRows))
	for _, rr := range readRows {
		if rr.Edges.Notification != nil {
			read[rr.Edges.Notification.ID] = true
		}
	}

	return rows, read, nil
}

func (r *Notifications) MarkRead(ctx context.Context, notificationID int, userID uint64, expiresAt time.Time) error {
	return r.MarkReadForIncarnation(ctx, notificationID, userID, "", expiresAt)
}
func (r *Notifications) MarkReadForIncarnation(ctx context.Context, notificationID int, userID uint64, incarnation string, expiresAt time.Time) error {
	return r.acknowledgeIncarnation(ctx, notificationID, userID, incarnation, expiresAt, false)
}
func (r *Notifications) acknowledgeIncarnation(ctx context.Context, notificationID int, userID uint64, incarnation string, expiresAt time.Time, peek bool) (err error) {
	tx, err := r.client.Tx(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if incarnation != "" {
		if err = lockNotificationIncarnation(ctx, tx, userID, incarnation); err != nil {
			return err
		}
	}
	upsert := tx.NotificationRead.Create().SetNotificationID(notificationID).SetUserID(userID).SetIncarnation(incarnation).SetExpiresAt(expiresAt).OnConflict()
	if peek {
		err = upsert.Ignore().Exec(ctx)
	} else {
		err = upsert.UpdateNewValues().Exec(ctx)
	}
	if err != nil {
		return err
	}
	return tx.Commit()
}
func receiptIncarnation(incarnation string, createdAt time.Time) predicate.NotificationRead {
	return notificationread.Or(notificationread.IncarnationEQ(incarnation), notificationread.And(notificationread.Or(notificationread.IncarnationEQ(""), notificationread.IncarnationIsNil()), notificationread.ReadAtGTE(createdAt)))
}
func lapsedForIncarnation(id uint64, incarnation string, createdAt, now time.Time) predicate.Notification {
	return notification.HasReadsWith(notificationread.UserIDEQ(id), receiptIncarnation(incarnation, createdAt), notificationread.ExpiresAtLTE(now))
}

func (r *Notifications) MarkPeeked(ctx context.Context, userID uint64, expiresAt time.Time) (int, error) {
	return r.MarkPeekedForIncarnation(ctx, userID, "", time.Time{}, expiresAt)
}
func (r *Notifications) MarkPeekedForIncarnation(ctx context.Context, userID uint64, incarnation string, createdAt time.Time, expiresAt time.Time) (int, error) {
	if incarnation != "" {
		rows, read, err := r.ListForIncarnation(ctx, userID, incarnation, createdAt, UserListLimit)
		if err != nil {
			return 0, err
		}
		count := 0
		for _, row := range rows {
			if read[row.ID] {
				continue
			}
			if err = r.acknowledgeIncarnation(ctx, row.ID, userID, incarnation, expiresAt, true); err != nil {
				return count, err
			}
			count++
		}
		return count, nil
	}

	now := time.Now()

	ids, err := db.WithQuery(ctx, func(ctx context.Context) ([]int, error) {
		return r.client.Notification.Query().
			Where(visibleForUser(userID, now)).
			Limit(UserListLimit).
			IDs(ctx)
	})
	if err != nil {
		return 0, err
	}
	if len(ids) == 0 {
		return 0, nil
	}

	readRows, err := db.WithQuery(ctx, func(ctx context.Context) ([]*ent.NotificationRead, error) {
		return r.client.NotificationRead.Query().
			Where(
				notificationread.UserIDEQ(userID),
				notificationread.HasNotificationWith(notification.IDIn(ids...)),
			).
			WithNotification().
			All(ctx)
	})
	if err != nil {
		return 0, err
	}
	already := make(map[int]bool, len(readRows))
	for _, rr := range readRows {
		if rr.Edges.Notification != nil {
			already[rr.Edges.Notification.ID] = true
		}
	}

	missing := make([]int, 0, len(ids))
	for _, id := range ids {
		if !already[id] {
			missing = append(missing, id)
		}
	}
	if len(missing) == 0 {
		return 0, nil
	}

	builders := make([]*ent.NotificationReadCreate, len(missing))
	for i, id := range missing {
		builders[i] = r.client.NotificationRead.Create().
			SetNotificationID(id).
			SetUserID(userID).
			SetExpiresAt(expiresAt)
	}

	err = db.WithExec(ctx, func(ctx context.Context) error {
		return r.client.NotificationRead.CreateBulk(builders...).Exec(ctx)
	})
	if err == nil {
		return len(missing), nil
	}
	if !ent.IsConstraintError(err) {
		return 0, err
	}

	peeked := 0
	for _, id := range missing {
		insErr := db.WithExec(ctx, func(ctx context.Context) error {
			return r.client.NotificationRead.Create().
				SetNotificationID(id).
				SetUserID(userID).
				SetExpiresAt(expiresAt).
				Exec(ctx)
		})
		if insErr == nil {
			peeked++
			continue
		}
		if !ent.IsConstraintError(insErr) {
			return peeked, insErr
		}
	}
	return peeked, nil
}

const CleanupBatchSize = 500

func (r *Notifications) DeleteExpired(ctx context.Context, now time.Time) (int, error) {
	ids, err := r.client.Notification.Query().Where(notification.ExpiresAtLTE(now)).Order(ent.Asc(notification.FieldExpiresAt), ent.Asc(notification.FieldID)).Limit(CleanupBatchSize).IDs(ctx)
	if err != nil || len(ids) == 0 {
		return 0, err
	}
	return r.client.Notification.Delete().Where(notification.IDIn(ids...), notification.ExpiresAtLTE(now)).Exec(ctx)
}

func (r *Notifications) Delete(ctx context.Context, id int) error {
	return db.WithExec(ctx, func(ctx context.Context) error {
		return r.client.Notification.DeleteOneID(id).Exec(ctx)
	})
}
