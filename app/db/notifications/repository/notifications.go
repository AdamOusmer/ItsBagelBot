// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository

import (
	"context"
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
	row, err := db.WithQuery(ctx, func(ctx context.Context) (*ent.Notification, error) {
		q := r.client.Notification.Create().
			SetScope(p.Scope).
			SetTitle(p.Title).
			SetBody(p.Body).
			SetLevel(p.Level).
			SetCreatedBy(p.CreatedBy).
			SetCreatedByLogin(p.CreatedByLogin).
			SetNillableTargetUserID(p.TargetUserID).
			SetNillableExpiresAt(p.ExpiresAt)
		if p.RequestID != "" {
			q.SetRequestID(p.RequestID)
		}
		return q.Save(ctx)
	})
	if err == nil {
		return row, true, nil
	}
	if !ent.IsConstraintError(err) || p.RequestID == "" {
		return nil, false, err
	}

	row, lookupErr := db.WithQuery(ctx, func(ctx context.Context) (*ent.Notification, error) {
		return r.client.Notification.Query().Where(notification.RequestIDEQ(p.RequestID)).Only(ctx)
	})
	if lookupErr != nil {
		return nil, false, lookupErr
	}
	return row, false, nil
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
	now := time.Now()

	rows, err := db.WithQuery(ctx, func(ctx context.Context) ([]*ent.Notification, error) {
		return r.client.Notification.Query().
			Where(
				visibleForUser(userID, now),
				notification.Not(lapsedForUser(userID, now)),
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
	return db.WithExec(ctx, func(ctx context.Context) error {
		err := r.client.NotificationRead.Create().
			SetNotificationID(notificationID).
			SetUserID(userID).
			SetExpiresAt(expiresAt).
			Exec(ctx)
		if err == nil {
			return nil
		}
		if !ent.IsConstraintError(err) {
			return err
		}
		_, updErr := r.client.NotificationRead.Update().
			Where(
				notificationread.UserIDEQ(userID),
				notificationread.HasNotificationWith(notification.IDEQ(notificationID)),
			).
			SetExpiresAt(expiresAt).
			Save(ctx)
		return updErr
	})
}

func (r *Notifications) MarkPeeked(ctx context.Context, userID uint64, expiresAt time.Time) (int, error) {
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

func (r *Notifications) DeleteExpired(ctx context.Context, now time.Time) (int, error) {
	return db.WithQuery(ctx, func(ctx context.Context) (int, error) {
		return r.client.Notification.Delete().
			Where(
				notification.ExpiresAtNotNil(),
				notification.ExpiresAtLTE(now),
			).
			Exec(ctx)
	})
}

func (r *Notifications) Delete(ctx context.Context, id int) error {
	return db.WithExec(ctx, func(ctx context.Context) error {
		return r.client.Notification.DeleteOneID(id).Exec(ctx)
	})
}
