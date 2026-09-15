// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package giveaway

import (
	"context"
	"errors"
	"testing"
	"time"

	"ItsBagelBot/app/db/transactions/ent"
	"ItsBagelBot/app/db/transactions/ent/awardemail"
	"ItsBagelBot/app/db/transactions/ent/enttest"
	"ItsBagelBot/app/db/transactions/mail"
	users "ItsBagelBot/internal/domain/rpc/users"
	"ItsBagelBot/internal/testdb"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type emailUsers struct {
	fakeUsers
	address string
	ref     *string
}

func (u *emailUsers) Coverage(context.Context, uint64) (users.PremiumCoverage, error) {
	return users.PremiumCoverage{RecurringReference: u.ref}, nil
}

func (u *emailUsers) Email(context.Context, uint64) (string, error) { return u.address, nil }

type ledgerMailer struct {
	templates []mail.GiveawayMessage
	sent      []mail.Delivery
	err       error
}

func (m *ledgerMailer) PrepareGiveaway(message mail.GiveawayMessage) (mail.PreparedContent, error) {
	m.templates = append(m.templates, message)
	return mail.PreparedContent{From: "from@example.com", Subject: "Prize", HTML: "<p>Prize</p>", Text: "Prize"}, nil
}

func (m *ledgerMailer) DeliverPrepared(_ context.Context, delivery mail.Delivery) (mail.Receipt, error) {
	m.sent = append(m.sent, delivery)
	return mail.Receipt{ProviderID: "receipt-1"}, m.err
}

type emailFixture struct {
	db     *ent.Client
	engine *Engine
	users  *emailUsers
	mailer *ledgerMailer
	now    time.Time
}

func newEmailFixture(t *testing.T) *emailFixture {
	t.Helper()
	client := enttest.Open(t, testdb.Driver, testdb.MemDSN(testdb.Name(t.Name())))
	t.Cleanup(func() { _ = client.Close() })
	f := &emailFixture{db: client, users: &emailUsers{address: "winner@example.com"}, mailer: &ledgerMailer{}, now: time.Date(2026, 9, 15, 12, 0, 0, 0, time.UTC)}
	f.engine = NewEngine(EngineConfig{Store: NewStore(client), Users: f.users, Mailer: f.mailer, Now: func() time.Time { return f.now }})
	_, err := client.GiveawayAward.Create().SetID("award").SetGiveawayID("campaign").SetDrawID("draw").SetUserID(42).SetOrdinal(1).SetPrizeMonths(3).SetIntervalRule("unverified").SetSelectedAt(f.now).Save(context.Background())
	require.NoError(t, err)
	return f
}

func (f *emailFixture) selection(t *testing.T) *ent.AwardEmail {
	t.Helper()
	row, err := f.db.AwardEmail.Query().Where(awardemail.KindEQ("selection")).Only(context.Background())
	require.NoError(t, err)
	return row
}

func TestSelectionSnapshotsSubscriberAndPendingContent(t *testing.T) {
	f := newEmailFixture(t)
	ref := "tbx-r-test"
	f.users.ref = &ref
	require.NoError(t, f.engine.awardEmail(context.Background(), "award", "selection"))
	require.Len(t, f.mailer.templates, 1)
	message := f.mailer.templates[0]
	assert.True(t, message.Subscriber)
	assert.True(t, message.BillingPending)
	assert.True(t, message.Start.IsZero())
	assert.True(t, message.End.IsZero())
	assert.Equal(t, 3, message.Months)
	row := f.selection(t)
	assert.Equal(t, "accepted", row.State)
	assert.NotContains(t, row.ContentJSON, f.users.address)
	assert.Equal(t, recipientHash(f.users.address), row.RecipientHash)
	assert.Equal(t, "accepted", f.db.GiveawayAward.GetX(context.Background(), "award").EmailState)
}

func TestUncertainEmailUsesOriginalWindowAndStopsAfterExpiry(t *testing.T) {
	f := newEmailFixture(t)
	f.mailer.err = errors.New("private recipient and provider request detail")
	require.ErrorIs(t, f.engine.awardEmail(context.Background(), "award", "selection"), mail.ErrUnconfirmed)
	first := f.selection(t).FirstAttemptAt
	require.NotNil(t, first)
	f.now = f.now.Add(23 * time.Hour)
	require.ErrorIs(t, f.engine.awardEmail(context.Background(), "award", "selection"), mail.ErrUnconfirmed)
	assert.Equal(t, first, f.selection(t).FirstAttemptAt)
	f.now = f.now.Add(2 * time.Hour)
	require.NoError(t, f.engine.awardEmail(context.Background(), "award", "selection"))
	assert.Equal(t, "needs_review", f.selection(t).State)
	assert.Len(t, f.mailer.sent, 2)
	assert.Len(t, f.mailer.templates, 1)
	assert.Equal(t, f.mailer.sent[0], f.mailer.sent[1])
	assert.NotContains(t, f.selection(t).LastError, "private")
}

func TestEmailContactChangeCannotReuseProviderIdentity(t *testing.T) {
	f := newEmailFixture(t)
	f.mailer.err = mail.ErrUnconfirmed
	require.Error(t, f.engine.awardEmail(context.Background(), "award", "selection"))
	f.users.address = "changed@example.com"
	require.NoError(t, f.engine.awardEmail(context.Background(), "award", "selection"))
	assert.Len(t, f.mailer.sent, 1)
	assert.Equal(t, "needs_review", f.selection(t).State)
}

func TestMissingContactRetainsPrizeAndRecovers(t *testing.T) {
	f := newEmailFixture(t)
	f.users.address = ""
	require.ErrorIs(t, f.engine.awardEmail(context.Background(), "award", "selection"), ErrMissingContact)
	assert.Empty(t, f.mailer.sent)
	assert.Nil(t, f.selection(t).FirstAttemptAt)
	award := f.db.GiveawayAward.GetX(context.Background(), "award")
	assert.Equal(t, "selected", award.State)
	assert.Equal(t, "missing_contact", award.EmailState)
	f.users.address = "winner@example.com"
	require.NoError(t, f.engine.awardEmail(context.Background(), "award", "selection"))
	assert.Equal(t, "accepted", f.selection(t).State)
	assert.Equal(t, "resolved", f.db.GiveawayAlert.Query().OnlyX(context.Background()).State)
}

func TestPendingSelectionGetsOneConfirmationAfterCommit(t *testing.T) {
	f := newEmailFixture(t)
	ctx := context.Background()
	require.NoError(t, f.engine.awardEmail(ctx, "award", "selection"))
	assert.Zero(t, f.db.GiveawayOutbox.Query().CountX(ctx))
	_, err := f.db.GiveawayAward.UpdateOneID("award").SetState("scheduled").SetBillingState("not_required").SetConfirmedStart(f.now).SetConfirmedEnd(f.now.AddDate(0, 3, 0)).Save(ctx)
	require.NoError(t, err)
	require.NoError(t, f.engine.reconcileEmails(ctx))
	require.NoError(t, f.engine.reconcileEmails(ctx))
	assert.Equal(t, 1, f.db.GiveawayOutbox.Query().CountX(ctx))
	require.NoError(t, f.engine.DispatchOnce(ctx))
	require.Len(t, f.mailer.templates, 2)
	assert.False(t, f.mailer.templates[1].BillingPending)
	assert.Equal(t, f.now, f.mailer.templates[1].Start)
	assert.NotEqual(t, f.mailer.sent[0].Key, f.mailer.sent[1].Key)
	assert.True(t, f.selection(t).ConfirmationQueued)
}
