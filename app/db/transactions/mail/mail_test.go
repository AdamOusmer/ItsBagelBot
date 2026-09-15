// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package mail

import (
	"context"
	"errors"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/resend/resend-go/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type recordingSender struct {
	request *resend.SendEmailRequest
	key     string
	reply   *resend.SendEmailResponse
	err     error
	calls   int
}

func (s *recordingSender) SendWithOptions(_ context.Context, request *resend.SendEmailRequest, options *resend.SendEmailOptions) (*resend.SendEmailResponse, error) {
	s.request, s.key = request, options.IdempotencyKey
	s.calls++
	return s.reply, s.err
}

func TestGiveawayDeliveryKeepsAcceptanceIdentityAndPendingCopy(t *testing.T) {
	sender := &recordingSender{reply: &resend.SendEmailResponse{Id: "email-1"}}
	m := &Mailer{sender: sender, from: "Bagel <test@example.com>", dashboardURL: "https://dashboard.example.com"}
	msg := GiveawayMessage{To: "winner@example.com", Months: 3, Subscriber: true, BillingPending: true, IdempotencyKey: "giveaway-award-1-selection-v1"}
	receipt, err := m.DeliverGiveaway(context.Background(), msg)
	require.NoError(t, err)
	assert.Equal(t, "email-1", receipt.ProviderID)
	assert.Equal(t, msg.IdempotencyKey, sender.key)
	assert.Equal(t, []string{msg.To}, sender.request.To)
	assert.Contains(t, sender.request.Subject, "3 months")
	assert.Contains(t, sender.request.Text, "recurring subscription")
	assert.Contains(t, sender.request.Text, "may still be charged")
	assert.NotContains(t, sender.request.Text, "0001")
	assert.Contains(t, sender.request.Html, "the folks behind the bagel")
	assert.Contains(t, sender.request.Html, "https://dashboard.example.com/billing")
	assert.Contains(t, sender.request.Html, `src="`+logoSrc+`"`)
	require.Len(t, sender.request.Attachments, 1)
	assert.Empty(t, sender.request.Attachments[0].Path)
	assert.Equal(t, logoCID, sender.request.Attachments[0].ContentId)
	assert.Equal(t, "image/png", sender.request.Attachments[0].ContentType)
	assert.Equal(t, []byte{0x89, 'P', 'N', 'G'}, sender.request.Attachments[0].Content[:4])
}

func TestDeliveryDoesNotExposeProviderErrorText(t *testing.T) {
	sender := &recordingSender{err: errors.New("rejected winner@example.com private-request-detail")}
	m := &Mailer{sender: sender}
	_, err := m.DeliverGiveaway(context.Background(), GiveawayMessage{To: "winner@example.com", Months: 1, IdempotencyKey: "award-1"})
	require.ErrorIs(t, err, ErrUnconfirmed)
	assert.NotContains(t, err.Error(), "winner@example.com")
	assert.NotContains(t, err.Error(), "private-request-detail")
}

func TestDeliveryRequiresAcceptanceAndStableIdentity(t *testing.T) {
	sender := &recordingSender{reply: &resend.SendEmailResponse{}}
	m := &Mailer{sender: sender}
	msg := GiveawayMessage{To: "winner@example.com", Months: 1, IdempotencyKey: "award-1"}
	_, err := m.DeliverGiveaway(context.Background(), msg)
	require.ErrorIs(t, err, ErrUnconfirmed)
	msg.IdempotencyKey = ""
	_, err = m.DeliverGiveaway(context.Background(), msg)
	require.ErrorIs(t, err, ErrInvalidMessage)
	assert.Equal(t, 1, sender.calls)
	assert.ErrorIs(t, safeDeliveryError(&resend.RateLimitError{Message: "recipient data"}), ErrRateLimited)
}

func TestGiftDeliveryRetainsExistingTemplateAndLinkDefense(t *testing.T) {
	sender := &recordingSender{reply: &resend.SendEmailResponse{Id: "gift-1"}}
	m := &Mailer{sender: sender, dashboardURL: "https://dashboard.example.com"}
	err := m.SendGift(context.Background(), GiftMessage{To: "winner@example.com", GiftedByLogin: "Friend", PersonalMessage: "Visit https://example.net", IdempotencyKey: "gift-1"})
	require.NoError(t, err)
	assert.Contains(t, sender.request.Html, "Someone just made your day.")
	assert.Contains(t, sender.request.Html, "Active on your account now")
	assert.Contains(t, sender.request.Html, "the folks behind the bagel")
	assert.NotContains(t, sender.request.Html, "example.net")
	assert.NotContains(t, sender.request.Text, "example.net")
}

func TestPendingGiveawayHidesUnconfirmedDates(t *testing.T) {
	start := time.Date(2026, 9, 20, 14, 0, 0, 0, time.UTC)
	end := start.AddDate(0, 3, 0)
	msg := GiveawayMessage{Months: 3, Start: start, End: end, Subscriber: true, BillingPending: true, NextPayment: &end}
	html, err := renderGiveawayHTML(msg, "https://dashboard.example.com")
	require.NoError(t, err)
	assert.NotContains(t, html, "September 20, 2026")
	assert.NotContains(t, html, "December 20, 2026")
	assert.NotContains(t, html, "protection is confirmed")
	assert.NotContains(t, html, "Active on your account now")
	assert.Contains(t, html, "Your full prize remains owed")
}

func TestGiveawayRejectsContradictoryConfirmedDates(t *testing.T) {
	start := time.Date(2026, 9, 20, 14, 0, 0, 0, time.UTC)
	msg := GiveawayMessage{Months: 3, Start: start, End: start.AddDate(0, 3, 0), NextPayment: &start}
	_, err := renderGiveawayHTML(msg, "https://dashboard.example.com")
	require.Error(t, err)
	msg.NextPayment, msg.End = nil, start
	_, err = renderGiveawayHTML(msg, "https://dashboard.example.com")
	require.Error(t, err)
}

func TestGiftAndGiveawayShareBrandedLayout(t *testing.T) {
	gift, err := giftHTML("Friend", "Enjoy!", "https://dashboard.example.com")
	require.NoError(t, err)
	giveaway, err := renderGiveawayHTML(GiveawayMessage{Months: 2}, "https://dashboard.example.com")
	require.NoError(t, err)
	for _, marker := range []string{logoSrc, "Staying safe.", "Access to beta features.", "the folks behind the bagel", "Every premium perk.", "width=\"560\""} {
		assert.True(t, strings.Contains(gift, marker), "gift is missing shared branding")
		assert.True(t, strings.Contains(giveaway, marker), "giveaway is missing shared branding")
	}
	assert.NotContains(t, gift, "feTurbulence")
	assert.NotContains(t, giveaway, "feTurbulence")
}

func TestSharedEmailLogoUsesEmbeddedPNG(t *testing.T) {
	parsed, err := url.Parse(legacyLogoURL)
	require.NoError(t, err)
	assert.Equal(t, "https", parsed.Scheme)
	assert.Equal(t, "itsbagelbot.com", parsed.Host)
	assert.Equal(t, "/logo.png", parsed.Path)
	assert.Equal(t, []byte{0x89, 'P', 'N', 'G'}, logoPNG[:4])
	assert.NotEmpty(t, logoPNG)

	html, err := giftHTML("Friend", "", "https://dashboard.example.com")
	require.NoError(t, err)
	assert.Contains(t, html, `src="`+logoSrc+`" width="52" height="52" alt="ItsBagelBot"`)
}
