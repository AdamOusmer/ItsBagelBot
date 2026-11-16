// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository

import (
	"context"
	"errors"

	"ItsBagelBot/app/db/notifications/ent"
	"ItsBagelBot/app/db/notifications/ent/notification"
	"ItsBagelBot/app/db/notifications/ent/notificationread"
	"ItsBagelBot/app/db/notifications/ent/predicate"

	"entgo.io/ent/dialect"
	"entgo.io/ent/dialect/sql"
)

type notificationDeletion struct {
	id    int
	found bool
}

func (d *notificationDeletion) apply(ctx context.Context, client *ent.Client) error {
	_, err := client.Notification.Query().Where(notification.IDEQ(d.id), lockNotification).Only(ctx)
	if ent.IsNotFound(err) && d.found {
		return nil
	}
	if err != nil {
		return err
	}
	d.found = true
	return drainNotifications(ctx, client, notification.IDEQ(d.id))
}

func lockNotification(s *sql.Selector) {
	if s.Dialect() != dialect.SQLite {
		s.ForUpdate()
	}
}

func deleteNotificationBatch(ctx context.Context, client *ent.Client, eligible predicate.Notification) (int, bool, error) {
	ids, err := client.Notification.Query().Where(eligible).Order(ent.Asc(notification.FieldExpiresAt), ent.Asc(notification.FieldID)).Limit(CleanupBatchSize).IDs(ctx)
	if err != nil || len(ids) == 0 {
		return 0, false, err
	}
	n, err := deleteSelectedNotifications(ctx, client, notification.And(notification.IDIn(ids...), eligible))
	return n, len(ids) == CleanupBatchSize, err
}

func deleteSelectedNotifications(ctx context.Context, client *ent.Client, selected predicate.Notification) (int, error) {
	if err := drainNotificationReads(ctx, client, notificationread.HasNotificationWith(selected)); err != nil {
		return 0, err
	}
	return client.Notification.Delete().Where(selected, notification.Not(notification.HasReads())).Exec(ctx)
}

func drainNotificationReads(ctx context.Context, client *ent.Client, eligible predicate.NotificationRead) error {
	for {
		ids, err := client.NotificationRead.Query().Where(eligible, lockNotification).Order(ent.Asc(notificationread.FieldID)).Limit(CleanupBatchSize).IDs(ctx)
		if err != nil || len(ids) == 0 {
			return err
		}
		n, err := client.NotificationRead.Delete().Where(notificationread.IDIn(ids...), eligible).Exec(ctx)
		if err != nil {
			return err
		}
		if n == 0 {
			return errors.New("notification read cleanup made no progress")
		}
	}
}

func drainNotifications(ctx context.Context, client *ent.Client, eligible predicate.Notification) error {
	for {
		ids, err := client.Notification.Query().Where(eligible, lockNotification).Order(ent.Asc(notification.FieldID)).Limit(CleanupBatchSize).IDs(ctx)
		if err != nil || len(ids) == 0 {
			return err
		}
		n, err := deleteSelectedNotifications(ctx, client, notification.And(notification.IDIn(ids...), eligible))
		if err != nil {
			return err
		}
		if n > 0 {
			continue
		}
		pending, err := notificationDrainPending(ctx, client, eligible)
		if err != nil || !pending {
			return err
		}
	}
}

func notificationDrainPending(ctx context.Context, client *ent.Client, eligible predicate.Notification) (bool, error) {
	reads, err := client.NotificationRead.Query().Where(notificationread.HasNotificationWith(eligible), lockNotification).Exist(ctx)
	if err != nil || reads {
		return reads, err
	}
	exists, err := client.Notification.Query().Where(eligible, lockNotification).Exist(ctx)
	if err != nil || !exists {
		return false, err
	}
	return false, errors.New("notification cleanup made no progress")
}
