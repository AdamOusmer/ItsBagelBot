package repository_test

import (
	"ItsBagelBot/app/db/notifications/ent"
	"ItsBagelBot/app/db/notifications/ent/notification"
	"ItsBagelBot/app/db/notifications/ent/notificationread"
	"ItsBagelBot/app/db/notifications/repository"
	"github.com/stretchr/testify/require"
	"strconv"
	"testing"
	"time"
)

func connection(now time.Time) repository.ConnectionParams {
	return repository.ConnectionParams{AccountID: alice, Incarnation: "123", IncidentID: "incident-1", Title: "Connection", Body: "Repair", Level: notification.LevelWarning, DeliveryDeadline: now.Add(time.Hour), ExpiresAt: now.Add(90 * 24 * time.Hour)}
}
func TestConnectionReceiptSurvivesDeleteAndCooldown(t *testing.T) {
	f := newFixture(t)
	now := time.Now()
	p := connection(now)
	first, err := f.repo.CreateConnection(f.ctx, p, now)
	require.NoError(t, err)
	require.True(t, first.Created)
	require.NoError(t, f.repo.Delete(f.ctx, first.NotificationID))
	replay, err := f.repo.CreateConnection(f.ctx, p, now.Add(time.Minute))
	require.NoError(t, err)
	require.Equal(t, "accepted", replay.Outcome)
	require.False(t, replay.Created)
	p.IncidentID = "incident-2"
	p.DeliveryDeadline = now.Add(30 * time.Hour)
	second, err := f.repo.CreateConnection(f.ctx, p, now.Add(23*time.Hour))
	require.NoError(t, err)
	require.Equal(t, "suppressed", second.Outcome)
	second, err = f.repo.CreateConnection(f.ctx, p, now.Add(25*time.Hour))
	require.NoError(t, err)
	require.Equal(t, "suppressed", second.Outcome)
	p.IncidentID = "incident-3"
	third, err := f.repo.CreateConnection(f.ctx, p, now.Add(25*time.Hour))
	require.NoError(t, err)
	require.True(t, third.Created)
}
func TestConnectionRetractionBeforeInsertAndExpiry(t *testing.T) {
	f := newFixture(t)
	now := time.Now()
	p := connection(now)
	_, err := f.repo.RetractConnection(f.ctx, p, now)
	require.NoError(t, err)
	result, err := f.repo.CreateConnection(f.ctx, p, now)
	require.NoError(t, err)
	require.Equal(t, "retracted", result.Outcome)
	p.IncidentID = "expired"
	result, err = f.repo.CreateConnection(f.ctx, p, p.DeliveryDeadline)
	require.NoError(t, err)
	require.Equal(t, "expired", result.Outcome)
	count, err := f.client.Notification.Query().Count(f.ctx)
	require.NoError(t, err)
	require.Zero(t, count)
}
func TestConnectionErasureFencesOnlyOldIncarnation(t *testing.T) {
	f := newFixture(t)
	now := time.Now()
	p := connection(now)
	_, err := f.repo.CreateConnection(f.ctx, p, now)
	require.NoError(t, err)
	require.NoError(t, f.repo.EraseConnection(f.ctx, alice, p.Incarnation, now))
	p.IncidentID = "late"
	result, err := f.repo.CreateConnection(f.ctx, p, now)
	require.NoError(t, err)
	require.Equal(t, "retracted", result.Outcome)
	p.Incarnation = "replacement"
	result, err = f.repo.CreateConnection(f.ctx, p, now)
	require.NoError(t, err)
	require.True(t, result.Created)
}

func TestConcurrentConnectionDeliveryAndRetraction(t *testing.T) {
	f := newFixture(t)
	now := time.Now()
	p := connection(now)
	start := make(chan struct{})
	errors := make(chan error, 12)
	for i := 0; i < 12; i++ {
		go func(retract bool) {
			<-start
			var err error
			// SQLite returns lock errors instead of waiting; retry only failed transactions,
			// as the durable outbox does after a transient delivery error.
			for attempt := 0; attempt < 100; attempt++ {
				if retract {
					_, err = f.repo.RetractConnection(f.ctx, p, now)
				} else {
					_, err = f.repo.CreateConnection(f.ctx, p, now)
				}
				if err == nil {
					break
				}
				time.Sleep(time.Millisecond)
			}
			errors <- err
		}(i == 0)
	}
	close(start)
	for i := 0; i < 12; i++ {
		require.NoError(t, <-errors)
	}
	result, ok, err := f.repo.ConnectionOutcome(f.ctx, p)
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, "retracted", result.Outcome)
	n, err := f.client.Notification.Query().Count(f.ctx)
	require.NoError(t, err)
	require.Zero(t, n)
	n, err = f.client.ConnectionReceipt.Query().Count(f.ctx)
	require.NoError(t, err)
	require.Equal(t, 1, n)
}
func TestCleanupUsesBoundedBatchesAndPreservesPermanent(t *testing.T) {
	f := newFixture(t)
	now := time.Now()
	past := now.Add(-time.Hour)
	for i := 0; i < 501; i++ {
		_, _, err := f.repo.Create(f.ctx, repository.CreateParams{Scope: notification.ScopeBroadcast, Title: "expired", Body: "body", Level: notification.LevelInfo, CreatedBy: 1, CreatedByLogin: "admin", ExpiresAt: &past})
		require.NoError(t, err)
	}
	_, _, err := f.repo.Create(f.ctx, repository.CreateParams{Scope: notification.ScopeBroadcast, Title: "permanent", Body: "body", Level: notification.LevelInfo, CreatedBy: 1, CreatedByLogin: "admin"})
	require.NoError(t, err)
	n, err := f.repo.DeleteExpired(f.ctx, now)
	require.NoError(t, err)
	require.Equal(t, 500, n)
	n, err = f.repo.DeleteExpired(f.ctx, now)
	require.NoError(t, err)
	require.Equal(t, 1, n)
	n, err = f.client.Notification.Query().Count(f.ctx)
	require.NoError(t, err)
	require.Equal(t, 1, n)
}

func TestConnectionExpiryRetainsDeliveryUntilOriginalHorizon(t *testing.T) {
	f := newFixture(t)
	now := time.Now()
	p := connection(now)
	p.ExpiresAt = now.Add(time.Hour)
	result, err := f.repo.CreateConnection(f.ctx, p, now)
	require.NoError(t, err)
	require.NoError(t, f.repo.MarkRead(f.ctx, result.NotificationID, alice, now.Add(time.Minute)))
	n, err := f.repo.DeleteExpired(f.ctx, now.Add(2*time.Hour))
	require.NoError(t, err)
	require.Equal(t, 1, n)
	replay, err := f.repo.CreateConnection(f.ctx, p, now.Add(2*time.Hour))
	require.NoError(t, err)
	require.Equal(t, "accepted", replay.Outcome)
	require.False(t, replay.Created)
	n, _, err = f.repo.CleanupDelivery(f.ctx, now.Add(2*time.Hour))
	require.NoError(t, err)
	require.Zero(t, n)
	n, _, err = f.repo.CleanupDelivery(f.ctx, p.DeliveryDeadline.Add(7*24*time.Hour))
	require.NoError(t, err)
	require.Equal(t, 1, n)
	replay, err = f.repo.CreateConnection(f.ctx, p, p.DeliveryDeadline.Add(7*24*time.Hour))
	require.NoError(t, err)
	require.Equal(t, "expired", replay.Outcome)
	require.False(t, replay.Created)
}

func TestErasureRemovesGenericAndLegacyHistoryWithoutTouchingReplacement(t *testing.T) {
	f := newFixture(t)
	now := time.Now()
	created := now.Add(-3 * time.Hour)
	deleted := now.Add(-time.Hour)
	old := strconv.FormatInt(created.UnixMicro(), 10)
	replacement := strconv.FormatInt(now.Add(-30*time.Minute).UnixMicro(), 10)
	makeDirect := func(inc string, at time.Time) *ent.Notification {
		return f.client.Notification.Create().SetScope(notification.ScopeDirect).SetTargetUserID(alice).SetIncarnation(inc).SetTitle("notice").SetBody("body").SetLevel(notification.LevelInfo).SetCreatedBy(1).SetCreatedByLogin("admin").SetCreatedAt(at).SaveX(f.ctx)
	}
	legacy := makeDirect("", now.Add(-2*time.Hour))
	generic := makeDirect(old, now.Add(-2*time.Hour))
	newDirect := makeDirect(replacement, now.Add(-15*time.Minute))
	newLegacy := makeDirect("", now.Add(-15*time.Minute))
	broadcast := f.client.Notification.Create().SetScope(notification.ScopeBroadcast).SetTitle("broadcast").SetBody("body").SetCreatedBy(1).SetCreatedByLogin("admin").SaveX(f.ctx)
	oldRead := f.client.NotificationRead.Create().SetUserID(alice).SetNotificationID(broadcast.ID).SetReadAt(now.Add(-2 * time.Hour)).SaveX(f.ctx)
	f.client.NotificationRead.Create().SetUserID(alice).SetIncarnation(old).SetNotificationID(newDirect.ID).SetReadAt(now.Add(-2 * time.Hour)).SaveX(f.ctx)
	newRead := f.client.NotificationRead.Create().SetUserID(alice).SetIncarnation(replacement).SetNotificationID(broadcast.ID).SaveX(f.ctx)
	require.NoError(t, f.repo.EraseConnection(f.ctx, alice, old, deleted))
	for _, id := range []int{legacy.ID, generic.ID} {
		require.False(t, f.client.Notification.Query().Where(notification.IDEQ(id)).ExistX(f.ctx))
	}
	for _, id := range []int{newDirect.ID, newLegacy.ID, broadcast.ID} {
		require.True(t, f.client.Notification.Query().Where(notification.IDEQ(id)).ExistX(f.ctx))
	}
	require.False(t, f.client.NotificationRead.Query().Where(notificationread.IDEQ(oldRead.ID)).ExistX(f.ctx))
	require.True(t, f.client.NotificationRead.Query().Where(notificationread.IDEQ(newRead.ID)).ExistX(f.ctx))
	require.NoError(t, f.repo.EraseConnection(f.ctx, alice, old, now.Add(time.Hour)), "retries retain immutable original cutoff")
	require.True(t, f.client.Notification.Query().Where(notification.IDEQ(newLegacy.ID)).ExistX(f.ctx))
	require.Error(t, f.repo.MarkReadForIncarnation(f.ctx, broadcast.ID, alice, old, now.Add(time.Hour)))
	_, _, err := f.repo.Create(f.ctx, repository.CreateParams{Incarnation: old, TargetUserID: &[]uint64{alice}[0], Scope: notification.ScopeDirect, Title: "late", Body: "body", Level: notification.LevelInfo, CreatedBy: 1, CreatedByLogin: "admin"})
	require.Error(t, err)
	rows, read, err := f.repo.ListForIncarnation(f.ctx, alice, replacement, now.Add(-30*time.Minute), 50)
	require.NoError(t, err)
	require.Len(t, rows, 3)
	require.True(t, read[broadcast.ID])
}
