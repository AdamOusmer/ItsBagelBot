package repository

import (
	"context"
	"time"

	"ItsBagelBot/app/notifications/ent"
	"ItsBagelBot/app/notifications/ent/notification"
	"ItsBagelBot/app/notifications/ent/notificationread"
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

// CreateParams describes one notification to record. TargetUserID is nil for
// scope=broadcast; RequestID (when set) deduplicates redelivered sends.
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

// Create records a notification. The bool reports whether a new row was
// inserted (false = an earlier delivery of the same RequestID won).
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

	// Another replica may have committed this logical send first. Return that
	// row as success so every RPC delivery produces the same reply without a
	// second insert or cache invalidation.
	row, lookupErr := db.WithQuery(ctx, func(ctx context.Context) (*ent.Notification, error) {
		return r.client.Notification.Query().Where(notification.RequestIDEQ(p.RequestID)).Only(ctx)
	})
	if lookupErr != nil {
		return nil, false, lookupErr
	}
	return row, false, nil
}

// ListForAdmin returns rows newest-first for the admin console.
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

// ListForUser returns the broadcast + direct-to-user notifications this user
// can see (newest first, expired rows excluded), plus the set of ids already
// acknowledged by that user.
func (r *Notifications) ListForUser(ctx context.Context, userID uint64, limit int) ([]*ent.Notification, map[int]bool, error) {
	now := time.Now()

	rows, err := db.WithQuery(ctx, func(ctx context.Context) ([]*ent.Notification, error) {
		return r.client.Notification.Query().
			Where(
				notification.Or(
					notification.ScopeEQ(notification.ScopeBroadcast),
					notification.TargetUserIDEQ(userID),
				),
				notification.Or(
					notification.ExpiresAtIsNil(),
					notification.ExpiresAtGT(now),
				),
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

// MarkRead records that userID has acknowledged notificationID. Idempotent:
// a repeat mark-read is a silent no-op rather than a unique-constraint error.
func (r *Notifications) MarkRead(ctx context.Context, notificationID int, userID uint64) error {
	err := db.WithExec(ctx, func(ctx context.Context) error {
		return r.client.NotificationRead.Create().
			SetNotificationID(notificationID).
			SetUserID(userID).
			Exec(ctx)
	})
	if err != nil && ent.IsConstraintError(err) {
		return nil
	}
	return err
}

// Delete retracts a notification (cascades to its read receipts).
func (r *Notifications) Delete(ctx context.Context, id int) error {
	return db.WithExec(ctx, func(ctx context.Context) error {
		return r.client.Notification.DeleteOneID(id).Exec(ctx)
	})
}
