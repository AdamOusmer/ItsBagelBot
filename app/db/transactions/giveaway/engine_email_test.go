// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package giveaway

import (
	"errors"
	"testing"
	"time"

	"ItsBagelBot/app/db/transactions/ent"
	"ItsBagelBot/app/db/transactions/ent/awardemail"
	"ItsBagelBot/app/db/transactions/ent/giveawayoutbox"
	"ItsBagelBot/app/db/transactions/mail"
	users "ItsBagelBot/internal/domain/rpc/users"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const winnerAddress = "winner@example.com"

type emailFixture struct {
	*fixture
	prize *ent.GiveawayAward
}

func newEmailFixture(t *testing.T) *emailFixture {
	t.Helper()
	f := newFixture(t)
	f.users, f.mailer = &fakeUsers{address: winnerAddress}, &ledgerMailer{}
	prize, err := f.newAward("award").SetPrizeMonths(3).SetIntervalRule("unverified").Save(t.Context())
	require.NoError(t, err)
	return &emailFixture{fixture: f, prize: prize}
}

func (f *emailFixture) selection() *ent.AwardEmail {
	f.t.Helper()
	row, err := f.db.AwardEmail.Query().Where(awardemail.KindEQ("selection")).Only(f.t.Context())
	require.NoError(f.t, err)
	return row
}

func (f *emailFixture) sendSelection() error { return f.dispatch(f.prize, "award.email.selection") }

func TestSelectionSnapshotsSubscriberAndPendingContent(t *testing.T) {
	f := newEmailFixture(t)
	ref := "tbx-r-test"
	f.users.coverage = users.PremiumCoverage{RecurringReference: &ref}

	require.NoError(t, f.sendSelection())

	require.Len(t, f.mailer.templates, 1)
	assert.Equal(t, mail.GiveawayMessage{Months: 3, Subscriber: true, BillingPending: true}, f.mailer.templates[0])
	row := f.selection()
	assert.Equal(t, "accepted", row.State)
	assert.NotContains(t, row.ContentJSON, winnerAddress)
	assert.NotEmpty(t, row.RecipientHash)
	assert.NotContains(t, row.RecipientHash, winnerAddress)
	assert.Equal(t, "accepted", f.db.GiveawayAward.GetX(t.Context(), "award").EmailState)
}

func TestUncertainEmailUsesOriginalWindowAndStopsAfterExpiry(t *testing.T) {
	f := newEmailFixture(t)
	f.mailer.err = errors.New("private recipient and provider request detail")
	require.ErrorIs(t, f.sendSelection(), mail.ErrUnconfirmed)
	first := f.selection().FirstAttemptAt
	require.NotNil(t, first)

	f.now = f.now.Add(23 * time.Hour)
	require.ErrorIs(t, f.sendSelection(), mail.ErrUnconfirmed)
	assert.Equal(t, first, f.selection().FirstAttemptAt)

	f.now = f.now.Add(2 * time.Hour)
	require.NoError(t, f.sendSelection())

	assert.Equal(t, "needs_review", f.selection().State)
	assert.Len(t, f.mailer.sent, 2)
	assert.Len(t, f.mailer.templates, 1)
	assert.Equal(t, f.mailer.sent[0], f.mailer.sent[1])
	assert.NotContains(t, f.selection().LastError, "private")
}

func TestEmailContactChangeCannotReuseProviderIdentity(t *testing.T) {
	f := newEmailFixture(t)
	f.mailer.err = mail.ErrUnconfirmed
	require.Error(t, f.sendSelection())

	f.users.address = "changed@example.com"
	require.NoError(t, f.sendSelection())

	assert.Len(t, f.mailer.sent, 1)
	assert.Equal(t, "needs_review", f.selection().State)
}

func TestMissingContactRetainsPrizeAndRecovers(t *testing.T) {
	f := newEmailFixture(t)
	f.users.address = ""
	require.ErrorIs(t, f.sendSelection(), ErrMissingContact)
	assert.Empty(t, f.mailer.sent)
	assert.Nil(t, f.selection().FirstAttemptAt)
	award := f.db.GiveawayAward.GetX(t.Context(), "award")
	assert.Equal(t, "selected", award.State)
	assert.Equal(t, "missing_contact", award.EmailState)

	f.users.address = winnerAddress
	require.NoError(t, f.sendSelection())

	assert.Equal(t, "accepted", f.selection().State)
	assert.Equal(t, "resolved", f.db.GiveawayAlert.Query().OnlyX(t.Context()).State)
}

func TestPendingSelectionGetsOneConfirmationAfterCommit(t *testing.T) {
	f := newEmailFixture(t)
	f.config = Config{BoundaryInterval: 5 * time.Millisecond}
	require.NoError(t, f.sendSelection())
	confirmations := func() int {
		return f.db.GiveawayOutbox.Query().Where(giveawayoutbox.EventTypeEQ("award.email.confirmation")).CountX(t.Context())
	}
	assert.Zero(t, confirmations())
	_, err := f.db.GiveawayAward.UpdateOneID("award").SetState("scheduled").SetBillingState("not_required").SetConfirmedStart(f.now).SetConfirmedEnd(f.now.AddDate(0, 3, 0)).Save(t.Context())
	require.NoError(t, err)

	f.runUntil(func() bool {
		return f.db.GiveawayOutbox.Query().Where(giveawayoutbox.EventTypeEQ("award.email.confirmation"), giveawayoutbox.StateEQ("completed")).CountX(t.Context()) == 1
	})

	assert.Equal(t, 1, confirmations(), "repeated passes must queue the confirmation once")
	require.Len(t, f.mailer.templates, 2)
	assert.False(t, f.mailer.templates[1].BillingPending)
	assert.Equal(t, f.now, f.mailer.templates[1].Start)
	require.Len(t, f.mailer.sent, 2)
	assert.NotEqual(t, f.mailer.sent[0].Key, f.mailer.sent[1].Key)
	assert.True(t, f.selection().ConfirmationQueued)
}
