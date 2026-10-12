package repository

import (
	"ItsBagelBot/app/db/notifications/ent"
	"ItsBagelBot/app/db/notifications/ent/connectionguard"
	"ItsBagelBot/app/db/notifications/ent/connectionreceipt"
	"ItsBagelBot/app/db/notifications/ent/notification"
	"ItsBagelBot/app/db/notifications/ent/notificationread"
	"context"
	"fmt"
	"strconv"
	"time"
)

// ConnectionParams binds all retries to the original incident and delivery window.
type ConnectionParams struct {
	AccountID                            uint64
	Incarnation, IncidentID, Title, Body string
	Level                                notification.Level
	DeliveryDeadline, ExpiresAt          time.Time
}
type ConnectionResult struct {
	Outcome        string
	NotificationID int
	Created        bool
}

func (p ConnectionParams) retention() time.Time {
	t := p.DeliveryDeadline.Add(7 * 24 * time.Hour)
	if p.ExpiresAt.After(t) {
		return p.ExpiresAt
	}
	return t
}

// connectionTx serializes both insertion and retraction on the same incarnation.
// A no-op upsert takes a SQL write lock, including when the guard already exists.
func (r *Notifications) connectionTx(ctx context.Context, p ConnectionParams, now time.Time, retract bool) (result ConnectionResult, err error) {
	if p.AccountID == 0 || p.Incarnation == "" || p.IncidentID == "" || p.DeliveryDeadline.IsZero() || p.ExpiresAt.IsZero() {
		return result, fmt.Errorf("connection identity and original deadlines required")
	}
	tx, err := r.client.Tx(ctx)
	if err != nil {
		return result, err
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()
	err = tx.ConnectionGuard.Create().SetAccountID(p.AccountID).SetIncarnation(p.Incarnation).SetRetainUntil(p.retention()).OnConflictColumns(connectionguard.FieldAccountID, connectionguard.FieldIncarnation).Update(func(u *ent.ConnectionGuardUpsert) { u.SetAccountID(p.AccountID) }).Exec(ctx)
	if err != nil {
		return result, err
	}
	guard, err := tx.ConnectionGuard.Query().Where(connectionguard.AccountIDEQ(p.AccountID), connectionguard.IncarnationEQ(p.Incarnation)).Only(ctx)
	if err != nil {
		return result, err
	}
	// Never shorten a guard's retention on a later incident.
	if p.retention().After(guard.RetainUntil) {
		if err = tx.ConnectionGuard.UpdateOne(guard).SetRetainUntil(p.retention()).Exec(ctx); err != nil {
			return result, err
		}
	}
	receipt, err := tx.ConnectionReceipt.Query().Where(connectionreceipt.AccountIDEQ(p.AccountID), connectionreceipt.IncarnationEQ(p.Incarnation), connectionreceipt.IncidentIDEQ(p.IncidentID)).Only(ctx)
	if err != nil && !ent.IsNotFound(err) {
		return result, err
	}
	err = nil
	if receipt != nil {
		result.Outcome = receipt.Outcome
		if receipt.NotificationID != nil {
			result.NotificationID = *receipt.NotificationID
		}
		if retract {
			if _, err = tx.Notification.Delete().Where(notification.TargetUserIDEQ(p.AccountID), notification.IncarnationEQ(p.Incarnation), notification.IncidentIDEQ(p.IncidentID)).Exec(ctx); err != nil {
				return result, err
			}
			err = tx.ConnectionReceipt.UpdateOne(receipt).SetOutcome("retracted").ClearNotificationID().Exec(ctx)
			result.Outcome = "retracted"
			result.NotificationID = 0
		}
		if err == nil {
			err = tx.Commit()
		}
		return result, err
	}
	outcome := "accepted"
	switch {
	case retract:
		outcome = "retracted"
	case guard.Erased:
		outcome = "retracted"
	case !now.Before(p.DeliveryDeadline) || !now.Before(p.ExpiresAt):
		outcome = "expired"
	case guard.LastCreatedAt != nil && now.Before(guard.LastCreatedAt.Add(24*time.Hour)):
		outcome = "suppressed"
	}
	var id *int
	if outcome == "accepted" {
		row, e := tx.Notification.Create().SetScope(notification.ScopeDirect).SetTargetUserID(p.AccountID).SetIncarnation(p.Incarnation).SetIncidentID(p.IncidentID).SetTitle(p.Title).SetBody(p.Body).SetLevel(p.Level).SetCreatedBy(0).SetCreatedByLogin("system").SetCreatedAt(now).SetExpiresAt(p.ExpiresAt).Save(ctx)
		if e != nil {
			err = e
			return result, err
		}
		id = &row.ID
		if err = tx.ConnectionGuard.UpdateOne(guard).SetLastCreatedAt(now).Exec(ctx); err != nil {
			return result, err
		}
		result.Created = true
		result.NotificationID = row.ID
	}
	_, err = tx.ConnectionReceipt.Create().SetAccountID(p.AccountID).SetIncarnation(p.Incarnation).SetIncidentID(p.IncidentID).SetOutcome(outcome).SetNillableNotificationID(id).SetDeliveryDeadline(p.DeliveryDeadline).SetExpiresAt(p.ExpiresAt).SetRetainUntil(p.retention()).Save(ctx)
	result.Outcome = outcome
	if err == nil {
		err = tx.Commit()
	}
	return result, err
}
func (r *Notifications) CreateConnection(ctx context.Context, p ConnectionParams, now time.Time) (ConnectionResult, error) {
	return r.connectionTx(ctx, p, now, false)
}
func (r *Notifications) RetractConnection(ctx context.Context, p ConnectionParams, now time.Time) (ConnectionResult, error) {
	return r.connectionTx(ctx, p, now, true)
}

func (r *Notifications) ConnectionOutcome(ctx context.Context, p ConnectionParams) (ConnectionResult, bool, error) {
	row, err := r.client.ConnectionReceipt.Query().Where(connectionreceipt.AccountIDEQ(p.AccountID), connectionreceipt.IncarnationEQ(p.Incarnation), connectionreceipt.IncidentIDEQ(p.IncidentID)).Only(ctx)
	if ent.IsNotFound(err) {
		return ConnectionResult{}, false, nil
	}
	if err != nil {
		return ConnectionResult{}, false, err
	}
	result := ConnectionResult{Outcome: row.Outcome}
	if row.NotificationID != nil {
		result.NotificationID = *row.NotificationID
	}
	return result, true, nil
}

// EraseConnection fences delayed delivery before deleting content for this incarnation.
// The tombstone outlives the maximum initial-delivery window.
func (r *Notifications) EraseConnection(ctx context.Context, accountID uint64, incarnation string, now time.Time) (err error) {
	if accountID == 0 || incarnation == "" || now.IsZero() {
		return fmt.Errorf("account incarnation required")
	}
	tx, err := r.client.Tx(ctx)
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()
	err = tx.ConnectionGuard.Create().SetAccountID(accountID).SetIncarnation(incarnation).SetRetainUntil(now.Add(8*24*time.Hour)).SetErasedAt(now).SetErased(true).OnConflictColumns(connectionguard.FieldAccountID, connectionguard.FieldIncarnation).Update(func(u *ent.ConnectionGuardUpsert) { u.SetErased(true); u.SetRetainUntil(now.Add(8 * 24 * time.Hour)) }).Exec(ctx)
	if err != nil {
		return err
	}
	guard, err := tx.ConnectionGuard.Query().Where(connectionguard.AccountIDEQ(accountID), connectionguard.IncarnationEQ(incarnation)).Only(ctx)
	if err != nil {
		return err
	}
	if guard.ErasedAt == nil {
		if err = tx.ConnectionGuard.UpdateOne(guard).SetErasedAt(now).Exec(ctx); err != nil {
			return err
		}
	} else {
		now = *guard.ErasedAt
	}
	created, err := strconv.ParseInt(incarnation, 10, 64)
	if err != nil {
		return err
	}
	createdAt := time.UnixMicro(created)
	if created <= 0 || now.Before(createdAt) {
		return fmt.Errorf("invalid incarnation erasure cutoff")
	}
	if _, err = tx.Notification.Delete().Where(notification.TargetUserIDEQ(accountID), notification.Or(notification.IncarnationEQ(incarnation), notification.And(notification.Or(notification.IncarnationEQ(""), notification.IncarnationIsNil()), notification.CreatedAtGTE(createdAt), notification.CreatedAtLTE(now)))).Exec(ctx); err != nil {
		return err
	}
	if _, err = tx.NotificationRead.Delete().Where(notificationread.UserIDEQ(accountID), notificationread.Or(notificationread.IncarnationEQ(incarnation), notificationread.And(notificationread.Or(notificationread.IncarnationEQ(""), notificationread.IncarnationIsNil()), notificationread.ReadAtGTE(createdAt), notificationread.ReadAtLTE(now)))).Exec(ctx); err != nil {
		return err
	}

	if _, err = tx.ConnectionReceipt.Update().Where(connectionreceipt.AccountIDEQ(accountID), connectionreceipt.IncarnationEQ(incarnation)).SetOutcome("retracted").ClearNotificationID().Save(ctx); err != nil {
		return err
	}
	return tx.Commit()
}
func (r *Notifications) CleanupDelivery(ctx context.Context, now time.Time) (int, bool, error) {
	ids, err := r.client.ConnectionReceipt.Query().Where(connectionreceipt.RetainUntilLTE(now)).Order(ent.Asc(connectionreceipt.FieldRetainUntil), ent.Asc(connectionreceipt.FieldID)).Limit(CleanupBatchSize).IDs(ctx)
	if err != nil {
		return 0, false, err
	}
	n := 0
	if len(ids) > 0 {
		n, err = r.client.ConnectionReceipt.Delete().Where(connectionreceipt.IDIn(ids...), connectionreceipt.RetainUntilLTE(now)).Exec(ctx)
		if err != nil {
			return n, false, err
		}
	}
	// All guard users take a write lock before inspecting it. Deletion is safe once
	// the retained delivery horizon and rolling cooldown have both elapsed.
	guards, err := r.client.ConnectionGuard.Query().Where(connectionguard.RetainUntilLTE(now), connectionguard.Or(connectionguard.LastCreatedAtIsNil(), connectionguard.LastCreatedAtLTE(now.Add(-24*time.Hour)))).Order(ent.Asc(connectionguard.FieldRetainUntil), ent.Asc(connectionguard.FieldID)).Limit(CleanupBatchSize).IDs(ctx)
	if err != nil {
		return n, false, err
	}
	if len(guards) > 0 {
		_, err = r.client.ConnectionGuard.Delete().Where(connectionguard.IDIn(guards...), connectionguard.RetainUntilLTE(now)).Exec(ctx)
		if err != nil {
			return n, false, err
		}
	}
	return n, len(ids) == CleanupBatchSize || len(guards) == CleanupBatchSize, nil
}
