// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package mail

import (
	"context"
	"testing"
	"time"

	"github.com/resend/resend-go/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPreparedGiveawayKeepsBodyAndSenderAcrossConfigurationChanges(t *testing.T) {
	sender := &recordingSender{reply: &resend.SendEmailResponse{Id: "receipt"}}
	m := &Mailer{sender: sender, from: "Original <original@example.com>", dashboardURL: "https://original.example.com"}
	content, err := m.PrepareGiveaway(GiveawayMessage{Months: 2, BillingPending: true})
	require.NoError(t, err)
	m.from, m.dashboardURL = "Changed <changed@example.com>", "https://changed.example.com"
	_, err = m.DeliverPrepared(context.Background(), Delivery{To: "winner@example.com", Key: "same-delivery", Content: content})
	require.NoError(t, err)
	assert.Equal(t, "Original <original@example.com>", sender.request.From)
	assert.Contains(t, sender.request.Html, "https://original.example.com/billing")
	assert.NotContains(t, sender.request.Html, "changed.example.com")
	assert.Equal(t, content.HTML, sender.request.Html)
}

func TestConfirmationAdaptsTheExistingLayout(t *testing.T) {
	start := time.Date(2026, 9, 15, 12, 0, 0, 0, time.UTC)
	m := &Mailer{from: "Bagel <bagel@example.com>", dashboardURL: "https://dashboard.example.com"}
	content, err := m.PrepareGiveaway(GiveawayMessage{Months: 2, Start: start, End: start.AddDate(0, 2, 0), Confirmation: true})
	require.NoError(t, err)
	assert.Contains(t, content.Subject, "confirmed")
	assert.Contains(t, content.HTML, "Your prize is confirmed.")
	assert.Contains(t, content.HTML, "the folks behind the bagel")
	assert.Contains(t, content.Text, "September 15, 2026 at 12:00 UTC")
}
