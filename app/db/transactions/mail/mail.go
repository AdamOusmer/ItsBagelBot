// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// Package mail renders transactional email and delivers it through Resend.
// The caller owns the delivery ledger; this package never stores recipients.
package mail

import (
	"context"
	_ "embed"
	"errors"
	"fmt"
	stdmail "net/mail"
	"strings"

	"ItsBagelBot/internal/domain/i18n"
	"ItsBagelBot/internal/domain/validate"

	"github.com/resend/resend-go/v4"
)

var (
	ErrInvalidMessage = errors.New("transactional email is invalid")
	ErrUnconfirmed    = errors.New("email provider acceptance is unconfirmed")
	ErrRateLimited    = errors.New("email provider rate limit reached")
)

// Receipt records provider acceptance, not inbox delivery. The durable caller
// saves this identifier together with its stable message identity.
type Receipt struct{ ProviderID string }

type emailSender interface {
	SendWithOptions(context.Context, *resend.SendEmailRequest, *resend.SendEmailOptions) (*resend.SendEmailResponse, error)
}

type Mailer struct {
	sender       emailSender
	from         string
	dashboardURL string
}

type GiftMessage struct {
	To              string
	GiftedByLogin   string
	PersonalMessage string
	IdempotencyKey  string
	Locale          string
}

type renderedMessage struct {
	To, From, Subject, HTML, Text, IdempotencyKey string
}

//go:embed assets/itsbagelbot-logo-v1.png
var logoPNG []byte

func New(apiKey, from, dashboardURL string) *Mailer {
	return &Mailer{sender: resend.NewClient(apiKey).Emails, from: from, dashboardURL: dashboardURL}
}

func (m *Mailer) SendGift(ctx context.Context, msg GiftMessage) error {
	// Notes are checked at checkout too. Keep this boundary in case a basket
	// was created outside the dashboard.
	if validate.ContainsLink(msg.PersonalMessage) {
		msg.PersonalMessage = ""
	}
	html, err := giftHTMLLocale(msg.GiftedByLogin, msg.PersonalMessage, m.dashboardURL, msg.Locale)
	if err != nil {
		return ErrInvalidMessage
	}
	subject := i18n.T(msg.Locale, "mail.subject.gift.generic")
	if msg.GiftedByLogin != "" {
		subject = fmt.Sprintf(i18n.T(msg.Locale, "mail.subject.gift.by"), msg.GiftedByLogin)
	}
	_, err = m.send(ctx, renderedMessage{
		To: msg.To, Subject: subject, HTML: html,
		Text: giftTextLocale(msg.GiftedByLogin, msg.PersonalMessage, m.dashboardURL, msg.Locale), IdempotencyKey: msg.IdempotencyKey,
	})
	return err
}

func (m *Mailer) SendGiveaway(ctx context.Context, msg GiveawayMessage) error {
	_, err := m.DeliverGiveaway(ctx, msg)
	return err
}

// DeliverGiveaway is used by the Transactions outbox so it can persist the
// acceptance ID. Retries must reuse both the message content and its key.
func (m *Mailer) DeliverGiveaway(ctx context.Context, msg GiveawayMessage) (Receipt, error) {
	content, err := m.PrepareGiveaway(msg)
	if err != nil {
		return Receipt{}, ErrInvalidMessage
	}
	return m.DeliverPrepared(ctx, Delivery{To: msg.To, Key: msg.IdempotencyKey, Content: content})
}

func (m *Mailer) send(ctx context.Context, msg renderedMessage) (Receipt, error) {
	if !msg.valid() {
		return Receipt{}, ErrInvalidMessage
	}
	if m == nil || m.sender == nil {
		return Receipt{}, ErrInvalidMessage
	}
	from := msg.From
	if from == "" {
		from = m.from
	}
	attachments := inlineLogoAttachments(msg.HTML)
	reply, err := m.sender.SendWithOptions(ctx, &resend.SendEmailRequest{
		From: from, To: []string{msg.To}, Subject: msg.Subject, Html: msg.HTML, Text: msg.Text,
		Attachments: attachments,
	}, &resend.SendEmailOptions{IdempotencyKey: msg.IdempotencyKey})
	if err != nil {
		return Receipt{}, safeDeliveryError(err)
	}
	if reply == nil || strings.TrimSpace(reply.Id) == "" {
		return Receipt{}, ErrUnconfirmed
	}
	return Receipt{ProviderID: reply.Id}, nil
}

func inlineLogoAttachments(html string) []*resend.Attachment {
	if !strings.Contains(html, `src="`+logoSrc+`"`) {
		return nil
	}
	return []*resend.Attachment{{
		Content: logoPNG, Filename: "itsbagelbot-logo.png", ContentType: "image/png", ContentId: logoCID,
	}}
}

func (msg renderedMessage) valid() bool {
	for _, field := range []string{msg.IdempotencyKey, msg.HTML, msg.Text} {
		if strings.TrimSpace(field) == "" {
			return false
		}
	}
	address, err := stdmail.ParseAddress(msg.To)
	return err == nil && address.Address == msg.To
}

func safeDeliveryError(err error) error {
	var limited *resend.RateLimitError
	if errors.As(err, &limited) {
		return ErrRateLimited
	}
	// Resend errors and network URLs can contain recipient data or request
	// details. Persist an allowlisted category, never wrap the provider text.
	return ErrUnconfirmed
}
