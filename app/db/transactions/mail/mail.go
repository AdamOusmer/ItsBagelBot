// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package mail

import (
	"context"
	_ "embed"
	"fmt"
	"strings"

	"ItsBagelBot/internal/domain/i18n"
	"ItsBagelBot/internal/domain/validate"
	"ItsBagelBot/pkg/email"
)

var (
	ErrInvalidMessage = email.ErrInvalidMessage
	ErrUnconfirmed    = email.ErrUnconfirmed
	ErrRateLimited    = email.ErrRateLimited
	ErrDisabled       = email.ErrDisabled
)

type Receipt struct{ ProviderID string }

type emailSender interface {
	Send(context.Context, email.Message) (email.Receipt, error)
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

func New(cfg email.Config, dashboardURL string) *Mailer {
	return &Mailer{sender: email.New(cfg), from: cfg.From, dashboardURL: dashboardURL}
}

func (m *Mailer) SendGift(ctx context.Context, msg GiftMessage) error {
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
	receipt, err := m.sender.Send(ctx, email.Message{
		From: from, To: msg.To, Subject: msg.Subject, HTML: msg.HTML, Text: msg.Text,
		IdempotencyKey: msg.IdempotencyKey, Attachments: inlineLogoAttachments(msg.HTML),
	})
	if err != nil {
		return Receipt{}, err
	}
	return Receipt{ProviderID: receipt.ID}, nil
}

func inlineLogoAttachments(html string) []email.Attachment {
	if !strings.Contains(html, `src="`+logoSrc+`"`) {
		return nil
	}
	return []email.Attachment{{
		Content: logoPNG, Filename: "itsbagelbot-logo.png", ContentType: "image/png", ContentId: logoCID,
	}}
}

func (msg renderedMessage) valid() bool {
	for _, field := range []string{msg.IdempotencyKey, msg.HTML, msg.Text} {
		if strings.TrimSpace(field) == "" {
			return false
		}
	}
	return validate.Email(msg.To) == nil
}
