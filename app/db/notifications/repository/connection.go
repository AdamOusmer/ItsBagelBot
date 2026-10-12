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
type connectionChange struct {
	Params  ConnectionParams
	Now     time.Time
	Retract bool
}

func (p ConnectionParams) retention() time.Time {
	t := p.DeliveryDeadline.Add(7 * 24 * time.Hour)
	if p.ExpiresAt.After(t) {
		return p.ExpiresAt
	}
	return t
}
func (p ConnectionParams) validate() error {
	if p.AccountID == 0 {
		return fmt.Errorf("account identity required")
	}
	if p.Incarnation == "" || p.IncidentID == "" {
		return fmt.Errorf("connection identity required")
	}
	if p.DeliveryDeadline.IsZero() || p.ExpiresAt.IsZero() {
		return fmt.Errorf("original connection deadlines required")
	}
	return nil
}

// connectionTx serializes both insertion and retraction on the same incarnation.
// A no-op upsert takes a SQL write lock, including when the guard already exists.
func (r *Notifications) connectionTx(ctx context.Context, change connectionChange) (ConnectionResult, error) {
	if err := change.Params.validate(); err != nil {
		return ConnectionResult{}, err
	}
	tx, err := r.client.Tx(ctx)
	if err != nil {
		return ConnectionResult{}, err
	}
	defer tx.Rollback()
	result, err := change.apply(ctx, tx)
	if err != nil {
		return result, err
	}
	return result, tx.Commit()
}
func (change connectionChange) apply(ctx context.Context, tx *ent.Tx) (ConnectionResult, error) {
	guard, err := lockConnectionGuard(ctx, tx, change.Params)
	if err != nil {
		return ConnectionResult{}, err
	}
	receipt, err := connectionReceipt(ctx, tx.Client(), change.Params)
	if err != nil {
		return ConnectionResult{}, err
	}
	if receipt != nil {
		return change.reuseReceipt(ctx, tx, receipt)
	}
	return change.recordDelivery(ctx, tx, guard)
}
func lockConnectionGuard(ctx context.Context, tx *ent.Tx, p ConnectionParams) (*ent.ConnectionGuard, error) {
	err := tx.ConnectionGuard.Create().SetAccountID(p.AccountID).SetIncarnation(p.Incarnation).SetRetainUntil(p.retention()).OnConflictColumns(connectionguard.FieldAccountID, connectionguard.FieldIncarnation).Update(func(u *ent.ConnectionGuardUpsert) { u.SetAccountID(p.AccountID) }).Exec(ctx)
	if err != nil {
		return nil, err
	}
	guard, err := tx.ConnectionGuard.Query().Where(connectionguard.AccountIDEQ(p.AccountID), connectionguard.IncarnationEQ(p.Incarnation)).Only(ctx)
	if err != nil {
		return nil, err
	}
	// Never shorten a guard's retention on a later incident.
	if p.retention().After(guard.RetainUntil) {
		err = tx.ConnectionGuard.UpdateOne(guard).SetRetainUntil(p.retention()).Exec(ctx)
	}
	return guard, err
}
func connectionReceipt(ctx context.Context, client *ent.Client, p ConnectionParams) (*ent.ConnectionReceipt, error) {
	row, err := client.ConnectionReceipt.Query().Where(connectionreceipt.AccountIDEQ(p.AccountID), connectionreceipt.IncarnationEQ(p.Incarnation), connectionreceipt.IncidentIDEQ(p.IncidentID)).Only(ctx)
	if ent.IsNotFound(err) {
		return nil, nil
	}
	return row, err
}
func resultOfReceipt(receipt *ent.ConnectionReceipt) ConnectionResult {
	result := ConnectionResult{Outcome: receipt.Outcome}
	if receipt.NotificationID != nil {
		result.NotificationID = *receipt.NotificationID
	}
	return result
}
func (change connectionChange) reuseReceipt(ctx context.Context, tx *ent.Tx, receipt *ent.ConnectionReceipt) (ConnectionResult, error) {
	if !change.Retract {
		return resultOfReceipt(receipt), nil
	}
	p := change.Params
	if _, err := tx.Notification.Delete().Where(notification.TargetUserIDEQ(p.AccountID), notification.IncarnationEQ(p.Incarnation), notification.IncidentIDEQ(p.IncidentID)).Exec(ctx); err != nil {
		return ConnectionResult{}, err
	}
	err := tx.ConnectionReceipt.UpdateOne(receipt).SetOutcome("retracted").ClearNotificationID().Exec(ctx)
	return ConnectionResult{Outcome: "retracted"}, err
}
func (change connectionChange) outcome(guard *ent.ConnectionGuard) string {
	if change.Retract || guard.Erased {
		return "retracted"
	}
	if !change.Now.Before(change.Params.DeliveryDeadline) {
		return "expired"
	}
	if !change.Now.Before(change.Params.ExpiresAt) {
		return "expired"
	}
	if guard.LastCreatedAt == nil {
		return "accepted"
	}
	if change.Now.Before(guard.LastCreatedAt.Add(24 * time.Hour)) {
		return "suppressed"
	}
	return "accepted"
}
func (change connectionChange) recordDelivery(ctx context.Context, tx *ent.Tx, guard *ent.ConnectionGuard) (ConnectionResult, error) {
	p := change.Params
	result := ConnectionResult{Outcome: change.outcome(guard)}
	var id *int
	if result.Outcome == "accepted" {
		row, err := change.createContent(ctx, tx, guard)
		if err != nil {
			return result, err
		}
		id = &row.ID
		result.Created = true
		result.NotificationID = row.ID
	}
	_, err := tx.ConnectionReceipt.Create().SetAccountID(p.AccountID).SetIncarnation(p.Incarnation).SetIncidentID(p.IncidentID).SetOutcome(result.Outcome).SetNillableNotificationID(id).SetDeliveryDeadline(p.DeliveryDeadline).SetExpiresAt(p.ExpiresAt).SetRetainUntil(p.retention()).Save(ctx)
	return result, err
}
func (change connectionChange) createContent(ctx context.Context, tx *ent.Tx, guard *ent.ConnectionGuard) (*ent.Notification, error) {
	p := change.Params
	row, err := tx.Notification.Create().SetScope(notification.ScopeDirect).SetTargetUserID(p.AccountID).SetIncarnation(p.Incarnation).SetIncidentID(p.IncidentID).SetTitle(p.Title).SetBody(p.Body).SetLevel(p.Level).SetCreatedBy(0).SetCreatedByLogin("system").SetCreatedAt(change.Now).SetExpiresAt(p.ExpiresAt).Save(ctx)
	if err != nil {
		return nil, err
	}
	return row, tx.ConnectionGuard.UpdateOne(guard).SetLastCreatedAt(change.Now).Exec(ctx)
}
func (r *Notifications) CreateConnection(ctx context.Context, p ConnectionParams, now time.Time) (ConnectionResult, error) {
	return r.connectionTx(ctx, connectionChange{Params: p, Now: now})
}
func (r *Notifications) RetractConnection(ctx context.Context, p ConnectionParams, now time.Time) (ConnectionResult, error) {
	return r.connectionTx(ctx, connectionChange{Params: p, Now: now, Retract: true})
}
func (r *Notifications) ConnectionOutcome(ctx context.Context, p ConnectionParams) (ConnectionResult, bool, error) {
	receipt, err := connectionReceipt(ctx, r.client, p)
	if err != nil {
		return ConnectionResult{}, false, err
	}
	if receipt == nil {
		return ConnectionResult{}, false, nil
	}
	return resultOfReceipt(receipt), true, nil
}

type notificationErasure struct {
	Recipient Recipient
	DeletedAt time.Time
}

func parseNotificationErasure(accountID uint64, incarnation string, deletedAt time.Time) (notificationErasure, error) {
	erasure := notificationErasure{Recipient: Recipient{UserID: accountID, Incarnation: incarnation}, DeletedAt: deletedAt}
	if accountID == 0 {
		return erasure, fmt.Errorf("account identity required")
	}
	if deletedAt.IsZero() {
		return erasure, fmt.Errorf("account erasure cutoff required")
	}
	created, err := strconv.ParseInt(incarnation, 10, 64)
	if err != nil {
		return erasure, err
	}
	erasure.Recipient.CreatedAt = time.UnixMicro(created)
	if created <= 0 {
		return erasure, fmt.Errorf("account incarnation required")
	}
	if deletedAt.Before(erasure.Recipient.CreatedAt) {
		return erasure, fmt.Errorf("invalid incarnation erasure cutoff")
	}
	return erasure, nil
}

// EraseConnection serializes content/history deletion with delivery and acknowledgment.
// The persisted cutoff is immutable across resumable erasure attempts.
func (r *Notifications) EraseConnection(ctx context.Context, accountID uint64, incarnation string, deletedAt time.Time) error {
	erasure, err := parseNotificationErasure(accountID, incarnation, deletedAt)
	if err != nil {
		return err
	}
	tx, err := r.client.Tx(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err = erasure.lockGuard(ctx, tx); err != nil {
		return err
	}
	if err = erasure.deleteHistory(ctx, tx); err != nil {
		return err
	}
	return tx.Commit()
}
func (erasure *notificationErasure) lockGuard(ctx context.Context, tx *ent.Tx) error {
	p := erasure.Recipient
	deadline := erasure.DeletedAt.Add(8 * 24 * time.Hour)
	err := tx.ConnectionGuard.Create().SetAccountID(p.UserID).SetIncarnation(p.Incarnation).SetRetainUntil(deadline).SetErasedAt(erasure.DeletedAt).SetErased(true).OnConflictColumns(connectionguard.FieldAccountID, connectionguard.FieldIncarnation).Update(func(u *ent.ConnectionGuardUpsert) { u.SetErased(true); u.SetRetainUntil(deadline) }).Exec(ctx)
	if err != nil {
		return err
	}
	guard, err := tx.ConnectionGuard.Query().Where(connectionguard.AccountIDEQ(p.UserID), connectionguard.IncarnationEQ(p.Incarnation)).Only(ctx)
	if err != nil {
		return err
	}
	if guard.ErasedAt != nil {
		erasure.DeletedAt = *guard.ErasedAt
		return nil
	}
	return tx.ConnectionGuard.UpdateOne(guard).SetErasedAt(erasure.DeletedAt).Exec(ctx)
}
func (erasure notificationErasure) deleteHistory(ctx context.Context, tx *ent.Tx) error {
	p := erasure.Recipient
	if _, err := tx.Notification.Delete().Where(notification.TargetUserIDEQ(p.UserID), notification.Or(notification.IncarnationEQ(p.Incarnation), notification.And(notification.Or(notification.IncarnationEQ(""), notification.IncarnationIsNil()), notification.CreatedAtGTE(p.CreatedAt), notification.CreatedAtLTE(erasure.DeletedAt)))).Exec(ctx); err != nil {
		return err
	}
	if _, err := tx.NotificationRead.Delete().Where(notificationread.UserIDEQ(p.UserID), notificationread.Or(notificationread.IncarnationEQ(p.Incarnation), notificationread.And(notificationread.Or(notificationread.IncarnationEQ(""), notificationread.IncarnationIsNil()), notificationread.ReadAtGTE(p.CreatedAt), notificationread.ReadAtLTE(erasure.DeletedAt)))).Exec(ctx); err != nil {
		return err
	}
	_, err := tx.ConnectionReceipt.Update().Where(connectionreceipt.AccountIDEQ(p.UserID), connectionreceipt.IncarnationEQ(p.Incarnation)).SetOutcome("retracted").ClearNotificationID().Save(ctx)
	return err
}
func (r *Notifications) CleanupDelivery(ctx context.Context, now time.Time) (int, bool, error) {
	deleted, more, err := r.cleanupConnectionReceipts(ctx, now)
	if err != nil {
		return deleted, false, err
	}
	guardsMore, err := r.cleanupConnectionGuards(ctx, now)
	return deleted, more || guardsMore, err
}
func (r *Notifications) cleanupConnectionReceipts(ctx context.Context, now time.Time) (int, bool, error) {
	ids, err := r.client.ConnectionReceipt.Query().Where(connectionreceipt.RetainUntilLTE(now)).Order(ent.Asc(connectionreceipt.FieldRetainUntil), ent.Asc(connectionreceipt.FieldID)).Limit(CleanupBatchSize).IDs(ctx)
	if err != nil {
		return 0, false, err
	}
	if len(ids) == 0 {
		return 0, false, nil
	}
	n, err := r.client.ConnectionReceipt.Delete().Where(connectionreceipt.IDIn(ids...), connectionreceipt.RetainUntilLTE(now)).Exec(ctx)
	return n, len(ids) == CleanupBatchSize, err
}
func (r *Notifications) cleanupConnectionGuards(ctx context.Context, now time.Time) (bool, error) {
	// Retention and cooldown must both have elapsed. The delete rechecks retention
	// so a delivery that extended a guard after selection cannot lose its lock.
	ids, err := r.client.ConnectionGuard.Query().Where(connectionguard.RetainUntilLTE(now), connectionguard.Or(connectionguard.LastCreatedAtIsNil(), connectionguard.LastCreatedAtLTE(now.Add(-24*time.Hour)))).Order(ent.Asc(connectionguard.FieldRetainUntil), ent.Asc(connectionguard.FieldID)).Limit(CleanupBatchSize).IDs(ctx)
	if err != nil {
		return false, err
	}
	if len(ids) == 0 {
		return false, nil
	}
	_, err = r.client.ConnectionGuard.Delete().Where(connectionguard.IDIn(ids...), connectionguard.RetainUntilLTE(now)).Exec(ctx)
	return len(ids) == CleanupBatchSize, err
}
