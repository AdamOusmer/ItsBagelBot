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

const dashboardURL = "https://dashboard.example.com"

var (
	sept15 = time.Date(2026, 9, 15, 12, 0, 0, 0, time.UTC)
	sept20 = time.Date(2026, 9, 20, 14, 0, 0, 0, time.UTC)
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

type rendered struct {
	subject, html, text string
}

type expectation struct {
	html, text, both          []string
	notHTML, notText, notBoth []string
	subject                   string
}

func (want expectation) check(t *testing.T, got rendered) {
	t.Helper()
	assert.Contains(t, got.subject, want.subject)
	for _, part := range want.html {
		assert.Contains(t, got.html, part)
	}
	for _, part := range want.text {
		assert.Contains(t, got.text, part)
	}
	for _, part := range want.both {
		assert.Contains(t, got.html, part)
		assert.Contains(t, got.text, part)
	}
	for _, part := range want.notHTML {
		assert.NotContains(t, got.html, part)
	}
	for _, part := range want.notText {
		assert.NotContains(t, got.text, part)
	}
	for _, part := range want.notBoth {
		assert.NotContains(t, got.html, part)
		assert.NotContains(t, got.text, part)
	}
}

func sendGift(t *testing.T, msg GiftMessage) (rendered, *resend.SendEmailRequest) {
	t.Helper()
	sender := &recordingSender{reply: &resend.SendEmailResponse{Id: "gift-1"}}
	msg.To, msg.IdempotencyKey = "winner@example.com", "gift-1"
	require.NoError(t, (&Mailer{sender: sender, dashboardURL: dashboardURL}).SendGift(context.Background(), msg))
	request := sender.request
	return rendered{subject: request.Subject, html: request.Html, text: request.Text}, request
}

func prepareGiveaway(t *testing.T, msg GiveawayMessage) rendered {
	t.Helper()
	content, err := (&Mailer{from: "Bagel <bagel@example.com>", dashboardURL: dashboardURL}).PrepareGiveaway(msg)
	require.NoError(t, err)
	return rendered{subject: content.Subject, html: content.HTML, text: content.Text}
}

var englishCopy = []string{"2 months", "September", "November", "recurring subscription", "Next scheduled payment", "View your prize", "Staying safe", " of ItsBagelBot Premium"}

func TestGiftEmailContent(t *testing.T) {
	frenchNote := "Someone just made your day. <b>Gift</b>"
	frenchChrome := []string{"1 mois de Premium ItsBagelBot", "Restez en sécurité.", "@itsbagelbot.com"}
	frenchLeaks := []string{"%!(", "%!s", " gifted you", " wrote", " of ItsBagelBot Premium", "See what's new", "Sent by ItsBagelBot"}
	for _, tc := range []struct {
		name string
		msg  GiftMessage
		want expectation
	}{
		{
			name: "renders the personal note",
			msg:  GiftMessage{GiftedByLogin: "mavey", PersonalMessage: "happy streaming!"},
			want: expectation{both: []string{"happy streaming!", "mavey wrote"}},
		},
		{
			name: "escapes the note in markup",
			msg:  GiftMessage{PersonalMessage: `<script>alert(1)</script>`},
			want: expectation{html: []string{"&lt;script&gt;", "A note for you"}, notHTML: []string{"<script>alert(1)</script>"}},
		},
		{
			name: "omits the note block when empty",
			msg:  GiftMessage{GiftedByLogin: "mavey"},
			want: expectation{notBoth: []string{"wrote", "A note for you"}},
		},
		{
			name: "TestGiftDeliveryRetainsExistingTemplateAndLinkDefense",
			msg:  GiftMessage{GiftedByLogin: "Friend", PersonalMessage: "Visit https://example.net"},
			want: expectation{
				html:    []string{"Someone just made your day.", "Active on your account now", "the folks behind the bagel"},
				notBoth: []string{"example.net"},
			},
		},
		{
			name: "TestFrenchGiftDeliveryUsesRecipientLocale",
			msg:  GiftMessage{Locale: "fr", GiftedByLogin: "Gift", PersonalMessage: "Someone just made your day."},
			want: expectation{
				subject: "offert",
				html:    []string{"Quelqu&#39;un vient d&#39;égayer votre journée.", "Someone just made your day.", "File prioritaire.", "Restez en sécurité."},
				text:    []string{"Vous avez Premium"},
			},
		},
		{
			name: "TestFrenchGiftCopiesPreservePersonalData anonymous",
			msg:  GiftMessage{Locale: "fr", PersonalMessage: frenchNote},
			want: expectation{
				html:    []string{`lang="fr"`, "Someone just made your day. &lt;b&gt;Gift&lt;/b&gt;"},
				text:    []string{frenchNote, "Vous avez reçu"},
				both:    frenchChrome,
				notBoth: frenchLeaks,
			},
		},
		{
			name: "TestFrenchGiftCopiesPreservePersonalData named",
			msg:  GiftMessage{Locale: "fr", GiftedByLogin: "Gift", PersonalMessage: frenchNote},
			want: expectation{
				html:    []string{`lang="fr"`, "Someone just made your day. &lt;b&gt;Gift&lt;/b&gt;"},
				text:    []string{frenchNote, "Gift a écrit"},
				both:    frenchChrome,
				notBoth: frenchLeaks,
			},
		},
		{
			name: "TestUnknownMailLocaleFallsBackToEnglish",
			msg:  GiftMessage{Locale: "unknown"},
			want: expectation{html: []string{`lang="en"`, "1 month of ItsBagelBot Premium"}, notHTML: []string{"mail.gift.", "mail.safety."}},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, _ := sendGift(t, tc.msg)
			tc.want.check(t, got)
		})
	}
}

func TestGiveawayEmailContent(t *testing.T) {
	end := sept20.AddDate(0, 3, 0)
	frenchEnd := sept15.AddDate(0, 2, 0)
	for _, tc := range []struct {
		name string
		msg  GiveawayMessage
		want expectation
	}{
		{
			name: "TestGiveawayTemplatePendingDoesNotInventDates",
			msg:  GiveawayMessage{Months: 3, Subscriber: true, BillingPending: true},
			want: expectation{
				html:    []string{"3 months", "recurring subscription", "being arranged", "Billing protection is still being checked"},
				text:    []string{"recurring subscription"},
				notHTML: []string{"January 1, 0001"},
				notText: []string{"next payment"},
			},
		},
		{
			name: "TestPendingGiveawayHidesUnconfirmedDates",
			msg:  GiveawayMessage{Months: 3, Start: sept20, End: end, Subscriber: true, BillingPending: true, NextPayment: &end},
			want: expectation{
				html:    []string{"Your full prize remains owed"},
				notHTML: []string{"September 20, 2026", "December 20, 2026", "protection is confirmed", "Active on your account now"},
			},
		},
		{
			name: "TestGiveawayTemplateConfirmedDateAndNextPayment",
			msg:  GiveawayMessage{Months: 3, Start: sept20, End: end, NextPayment: &end},
			want: expectation{html: []string{"September 20, 2026", "December 20, 2026", "Next scheduled payment"}},
		},
		{
			name: "TestConfirmationAdaptsTheExistingLayout",
			msg:  GiveawayMessage{Months: 2, Start: sept15, End: frenchEnd, Confirmation: true},
			want: expectation{
				subject: "confirmed",
				html:    []string{"Your prize is confirmed.", "the folks behind the bagel"},
				text:    []string{"September 15, 2026 at 12:00 UTC"},
			},
		},
		{
			name: "TestFrenchGiveawayDeliveryUsesRecipientLocale",
			msg:  GiveawayMessage{Months: 2, Locale: "fr", Start: sept15, End: frenchEnd},
			want: expectation{
				subject: "gagné",
				html:    []string{"Vous avez gagné Premium", "Votre période Premium est confirmée", "Tous les avantages Premium.", "Restez en sécurité."},
				text:    []string{"Vous avez gagné"},
			},
		},
		{
			name: "TestFrenchGiveawayAllBillingStates confirmed",
			msg:  GiveawayMessage{Months: 2, Locale: "fr", Start: sept15, End: frenchEnd, Subscriber: true, NextPayment: &frenchEnd},
			want: expectation{
				subject: "2 mois",
				both:    []string{"2 mois", "abonnement récurrent", "15 septembre 2026 à 12:00 UTC", "Prochain paiement prévu", "15 novembre 2026 à 12:00 UTC"},
				notBoth: englishCopy,
			},
		},
		{
			name: "TestFrenchGiveawayAllBillingStates pending",
			msg:  GiveawayMessage{Months: 2, Locale: "fr", Start: sept15, End: frenchEnd, Subscriber: true, BillingPending: true, NextPayment: &frenchEnd},
			want: expectation{
				subject: "2 mois",
				both:    []string{"2 mois", "abonnement récurrent", "peut donc être débité"},
				notBoth: append([]string{"15 septembre"}, englishCopy...),
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tc.want.check(t, prepareGiveaway(t, tc.msg))
		})
	}
}

func TestPrepareGiveawayRejectsContradictoryConfirmedDates(t *testing.T) {
	for _, tc := range []struct {
		name string
		msg  GiveawayMessage
	}{
		{"next payment before the prize ends", GiveawayMessage{Months: 3, Start: sept20, End: sept20.AddDate(0, 3, 0), NextPayment: &sept20}},
		{"prize ending when it starts", GiveawayMessage{Months: 3, Start: sept20, End: sept20}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := (&Mailer{dashboardURL: dashboardURL}).PrepareGiveaway(tc.msg)
			require.ErrorIs(t, err, ErrInvalidMessage)
		})
	}
}

func TestGiftAndGiveawayShareBrandedLayout(t *testing.T) {
	gift, _ := sendGift(t, GiftMessage{GiftedByLogin: "Friend", PersonalMessage: "Enjoy!"})
	giveaway := prepareGiveaway(t, GiveawayMessage{Months: 2})
	markers := []string{logoSrc, "Staying safe.", "Access to beta features.", "the folks behind the bagel", "Every premium perk.", `width="560"`}
	for _, email := range []rendered{gift, giveaway} {
		for _, marker := range markers {
			assert.Contains(t, email.html, marker)
		}
		assert.NotContains(t, email.html, "feTurbulence")
	}
}

func TestSharedEmailLogoUsesEmbeddedPNG(t *testing.T) {
	parsed, err := url.Parse(legacyLogoURL)
	require.NoError(t, err)
	assert.Equal(t, "https", parsed.Scheme)
	assert.Equal(t, "itsbagelbot.com", parsed.Host)
	assert.Equal(t, "/logo.png", parsed.Path)
	assert.Equal(t, []byte{0x89, 'P', 'N', 'G'}, logoPNG[:4])
	assert.NotEmpty(t, logoPNG)

	gift, request := sendGift(t, GiftMessage{GiftedByLogin: "Friend"})
	assert.Contains(t, gift.html, `src="`+logoSrc+`" width="52" height="52" alt="ItsBagelBot"`)
	require.Len(t, request.Attachments, 1)
	assert.Equal(t, logoCID, request.Attachments[0].ContentId)
}

func TestGiveawayDeliveryKeepsAcceptanceIdentityAndPendingCopy(t *testing.T) {
	sender := &recordingSender{reply: &resend.SendEmailResponse{Id: "email-1"}}
	m := &Mailer{sender: sender, from: "Bagel <test@example.com>", dashboardURL: dashboardURL}
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
	assert.Contains(t, sender.request.Html, dashboardURL+"/billing")
	assert.Contains(t, sender.request.Html, `src="`+logoSrc+`"`)
	require.Len(t, sender.request.Attachments, 1)
	attachment := sender.request.Attachments[0]
	assert.Empty(t, attachment.Path)
	assert.Equal(t, logoCID, attachment.ContentId)
	assert.Equal(t, "image/png", attachment.ContentType)
	assert.Equal(t, []byte{0x89, 'P', 'N', 'G'}, attachment.Content[:4])
}

func TestSendGiveawayDeliversOnceWithItsIdempotencyKey(t *testing.T) {
	sender := &recordingSender{reply: &resend.SendEmailResponse{Id: "email-1"}}
	m := &Mailer{sender: sender, from: "Bagel <test@example.com>", dashboardURL: dashboardURL}

	err := m.SendGiveaway(context.Background(), GiveawayMessage{To: "winner@example.com", Months: 2, IdempotencyKey: "award-2"})

	require.NoError(t, err)
	assert.Equal(t, 1, sender.calls)
	assert.Equal(t, "award-2", sender.key)
}

func TestDeliveryFailures(t *testing.T) {
	for _, tc := range []struct {
		name      string
		sender    *recordingSender
		key       string
		wantErr   error
		wantCalls int
		leaks     []string
	}{
		{
			name:      "hides provider error text",
			sender:    &recordingSender{err: errors.New("rejected winner@example.com private-request-detail")},
			key:       "award-1",
			wantErr:   ErrUnconfirmed,
			wantCalls: 1,
			leaks:     []string{"winner@example.com", "private-request-detail"},
		},
		{
			name:      "requires an acceptance id",
			sender:    &recordingSender{reply: &resend.SendEmailResponse{}},
			key:       "award-1",
			wantErr:   ErrUnconfirmed,
			wantCalls: 1,
		},
		{
			name:    "requires a stable idempotency key before sending",
			sender:  &recordingSender{reply: &resend.SendEmailResponse{Id: "email-1"}},
			wantErr: ErrInvalidMessage,
		},
		{
			name:      "maps provider rate limiting without recipient data",
			sender:    &recordingSender{err: &resend.RateLimitError{Message: "recipient data"}},
			key:       "award-1",
			wantErr:   ErrRateLimited,
			wantCalls: 1,
			leaks:     []string{"recipient data"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := &Mailer{sender: tc.sender}
			_, err := m.DeliverGiveaway(context.Background(), GiveawayMessage{To: "winner@example.com", Months: 1, IdempotencyKey: tc.key})
			require.ErrorIs(t, err, tc.wantErr)
			assert.Equal(t, tc.wantCalls, tc.sender.calls)
			for _, leak := range tc.leaks {
				assert.NotContains(t, err.Error(), leak)
			}
		})
	}
}

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
	require.Len(t, sender.request.Attachments, 1)
	assert.Empty(t, sender.request.Attachments[0].Path)
	assert.Equal(t, logoCID, sender.request.Attachments[0].ContentId)
}

func TestPreparedLegacyBodyDoesNotGainNewLogoAttachment(t *testing.T) {
	sender := &recordingSender{reply: &resend.SendEmailResponse{Id: "receipt"}}
	m := &Mailer{sender: sender, from: "Original <original@example.com>", dashboardURL: "https://original.example.com"}
	content, err := m.PrepareGiveaway(GiveawayMessage{Months: 2, BillingPending: true})
	require.NoError(t, err)
	content.HTML = strings.Replace(content.HTML, `src="`+logoSrc+`"`, `src="`+legacyLogoURL+`"`, 1)

	_, err = m.DeliverPrepared(context.Background(), Delivery{To: "winner@example.com", Key: "legacy-delivery", Content: content})

	require.NoError(t, err)
	assert.Empty(t, sender.request.Attachments)
}
