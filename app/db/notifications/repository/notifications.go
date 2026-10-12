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

type Recipient struct {
	UserID      uint64
	Incarnation string
	CreatedAt   time.Time
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
	if p.Incarnation == "" || p.TargetUserID == nil {
		return createNotification(ctx, r.client, p)
	}
	tx, err := r.client.Tx(ctx)
	if err != nil {
		return nil, false, err
	}
	defer tx.Rollback()
	if err = lockNotificationIncarnation(ctx, tx, Recipient{UserID: *p.TargetUserID, Incarnation: p.Incarnation}); err != nil {
		return nil, false, err
	}
	row, created, err := createNotification(ctx, tx.Client(), p)
	if err != nil {
		return nil, false, err
	}
	return row, created, tx.Commit()
}
func createNotification(ctx context.Context, client *ent.Client, p CreateParams) (*ent.Notification, bool, error) {
	q := client.Notification.Create().SetScope(p.Scope).SetTitle(p.Title).SetBody(p.Body).SetLevel(p.Level).SetCreatedBy(p.CreatedBy).SetCreatedByLogin(p.CreatedByLogin).SetNillableTargetUserID(p.TargetUserID).SetNillableExpiresAt(p.ExpiresAt).SetIncarnation(p.Incarnation)
	if p.RequestID != "" {
		q.SetRequestID(p.RequestID)
	}
	row, err := q.Save(ctx)
	if err == nil {
		return row, true, nil
	}
	if !ent.IsConstraintError(err) || p.RequestID == "" {
		return nil, false, err
	}
	row, err = client.Notification.Query().Where(notification.RequestIDEQ(p.RequestID)).Only(ctx)
	return row, false, err
}

// Both acknowledgement and direct-send transactions serialize against erasure.
func lockNotificationIncarnation(ctx context.Context, tx *ent.Tx, recipient Recipient) error {
	id, incarnation := recipient.UserID, recipient.Incarnation
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
	return r.ListForIncarnation(ctx, Recipient{UserID: userID}, limit)
}
func (r *Notifications) ListForIncarnation(ctx context.Context, recipient Recipient, limit int) ([]*ent.Notification, map[int]bool, error) {
	now := time.Now()
	userID, incarnation, createdAt := recipient.UserID, recipient.Incarnation, recipient.CreatedAt

	rows, err := db.WithQuery(ctx, func(ctx context.Context) ([]*ent.Notification, error) {
		return r.client.Notification.Query().
			Where(
				visibleForUser(userID, now),
				notification.Or(notification.ScopeEQ(notification.ScopeBroadcast), notification.IncarnationEQ(incarnation), notification.And(notification.Or(notification.IncarnationEQ(""), notification.IncarnationIsNil()), notification.CreatedAtGTE(createdAt))),
				notification.Not(lapsedForIncarnation(recipient, now)),
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
				receiptIncarnation(recipient),
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

type readAcknowledgment struct {
	NotificationID int
	Recipient      Recipient
	ExpiresAt      time.Time
	Peek           bool
}

func (r *Notifications) MarkRead(ctx context.Context, notificationID int, userID uint64, expiresAt time.Time) error {
	return r.MarkReadForIncarnation(ctx, notificationID, Recipient{UserID: userID}, expiresAt)
}
func (r *Notifications) MarkReadForIncarnation(ctx context.Context, notificationID int, recipient Recipient, expiresAt time.Time) error {
	return r.acknowledgeIncarnation(ctx, readAcknowledgment{NotificationID: notificationID, Recipient: recipient, ExpiresAt: expiresAt})
}
func (r *Notifications) acknowledgeIncarnation(ctx context.Context, ack readAcknowledgment) error {
	tx, err := r.client.Tx(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if ack.Recipient.Incarnation != "" {
		if err = lockNotificationIncarnation(ctx, tx, ack.Recipient); err != nil {
			return err
		}
	}
	upsert := tx.NotificationRead.Create().SetNotificationID(ack.NotificationID).SetUserID(ack.Recipient.UserID).SetIncarnation(ack.Recipient.Incarnation).SetExpiresAt(ack.ExpiresAt).OnConflict()
	if ack.Peek {
		err = upsert.Ignore().Exec(ctx)
	} else {
		err = upsert.UpdateNewValues().Exec(ctx)
	}
	if err != nil {
		return err
	}
	return tx.Commit()
}
func receiptIncarnation(recipient Recipient) predicate.NotificationRead {
	return notificationread.Or(notificationread.IncarnationEQ(recipient.Incarnation), notificationread.And(notificationread.Or(notificationread.IncarnationEQ(""), notificationread.IncarnationIsNil()), notificationread.ReadAtGTE(recipient.CreatedAt)))
}
func lapsedForIncarnation(recipient Recipient, now time.Time) predicate.Notification {
	return notification.HasReadsWith(notificationread.UserIDEQ(recipient.UserID), receiptIncarnation(recipient), notificationread.ExpiresAtLTE(now))
}

func (r *Notifications) MarkPeeked(ctx context.Context, userID uint64, expiresAt time.Time) (int, error) {
	return r.MarkPeekedForIncarnation(ctx, Recipient{UserID: userID}, expiresAt)
}
func (r *Notifications) MarkPeekedForIncarnation(ctx context.Context, recipient Recipient, expiresAt time.Time) (int, error) {
	if recipient.Incarnation == "" {
		return r.markLegacyPeeked(ctx, recipient, expiresAt)
	}
	rows, read, err := r.ListForIncarnation(ctx, recipient, UserListLimit)
	if err != nil {
		return 0, err
	}
	count := 0
	for _, row := range rows {
		if read[row.ID] {
			continue
		}
		ack := readAcknowledgment{NotificationID: row.ID, Recipient: recipient, ExpiresAt: expiresAt, Peek: true}
		if err = r.acknowledgeIncarnation(ctx, ack); err != nil {
			return count, err
		}
		count++
	}
	return count, nil
}
func (r *Notifications) markLegacyPeeked(ctx context.Context, recipient Recipient, expiresAt time.Time) (int, error) {
	ids, err := db.WithQuery(ctx, func(ctx context.Context) ([]int, error) {
		return r.client.Notification.Query().Where(visibleForUser(recipient.UserID, time.Now())).Limit(UserListLimit).IDs(ctx)
	})
	if err != nil || len(ids) == 0 {
		return 0, err
	}
	readRows, err := db.WithQuery(ctx, func(ctx context.Context) ([]*ent.NotificationRead, error) {
		return r.client.NotificationRead.Query().Where(notificationread.UserIDEQ(recipient.UserID), notificationread.HasNotificationWith(notification.IDIn(ids...))).WithNotification().All(ctx)
	})
	if err != nil {
		return 0, err
	}
	missing := unacknowledgedIDs(ids, readRows)
	if len(missing) == 0 {
		return 0, nil
	}
	return r.insertLegacyPeeks(ctx, recipient, missing, expiresAt)
}
func acknowledgedNotificationIDs(reads []*ent.NotificationRead) map[int]bool {
	already := make(map[int]bool, len(reads))
	for _, read := range reads {
		if read.Edges.Notification != nil {
			already[read.Edges.Notification.ID] = true
		}
	}
	return already
}
func unacknowledgedIDs(ids []int, reads []*ent.NotificationRead) []int {
	already := acknowledgedNotificationIDs(reads)
	missing := make([]int, 0, len(ids))
	for _, id := range ids {
		if !already[id] {
			missing = append(missing, id)
		}
	}
	return missing
}
func (r *Notifications) insertLegacyPeeks(ctx context.Context, recipient Recipient, ids []int, expiresAt time.Time) (int, error) {
	builders := make([]*ent.NotificationReadCreate, len(ids))
	for i, id := range ids {
		builders[i] = r.client.NotificationRead.Create().SetNotificationID(id).SetUserID(recipient.UserID).SetExpiresAt(expiresAt)
	}
	err := db.WithExec(ctx, func(ctx context.Context) error { return r.client.NotificationRead.CreateBulk(builders...).Exec(ctx) })
	if err == nil {
		return len(ids), nil
	}
	if !ent.IsConstraintError(err) {
		return 0, err
	}
	return r.insertLegacyPeeksIndividually(ctx, recipient, ids, expiresAt)
}
func (r *Notifications) insertLegacyPeeksIndividually(ctx context.Context, recipient Recipient, ids []int, expiresAt time.Time) (int, error) {
	peeked := 0
	for _, id := range ids {
		err := db.WithExec(ctx, func(ctx context.Context) error {
			return r.client.NotificationRead.Create().SetNotificationID(id).SetUserID(recipient.UserID).SetExpiresAt(expiresAt).Exec(ctx)
		})
		if err == nil {
			peeked++
			continue
		}
		if !ent.IsConstraintError(err) {
			return peeked, err
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
