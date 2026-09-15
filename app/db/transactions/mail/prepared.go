// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package mail

import "context"

const GiveawayTemplateVersion = "giveaway-v1"

// PreparedContent contains the rendered, recipient-free message. The outbox
// stores it before delivery so a deployment or configuration change cannot
// change the body associated with an existing provider idempotency key.
type PreparedContent struct {
	From    string `json:"from"`
	Subject string `json:"subject"`
	HTML    string `json:"html"`
	Text    string `json:"text"`
}

type Delivery struct {
	To, Key string
	Content PreparedContent
}

func (m *Mailer) PrepareGiveaway(msg GiveawayMessage) (PreparedContent, error) {
	if m == nil {
		return PreparedContent{}, ErrInvalidMessage
	}
	html, err := renderGiveawayHTML(msg, m.dashboardURL)
	if err != nil {
		return PreparedContent{}, ErrInvalidMessage
	}
	subject := "You won " + prizeMonthsText(msg.Months) + " of Premium 🥯"
	if msg.Confirmation {
		subject = "Your Premium giveaway prize is confirmed 🥯"
	}
	return PreparedContent{
		From: m.from, Subject: subject,
		HTML: html, Text: giveawayText(msg, m.dashboardURL),
	}, nil
}

func (m *Mailer) DeliverPrepared(ctx context.Context, delivery Delivery) (Receipt, error) {
	content := delivery.Content
	return m.send(ctx, renderedMessage{
		To: delivery.To, From: content.From, Subject: content.Subject,
		HTML: content.HTML, Text: content.Text, IdempotencyKey: delivery.Key,
	})
}
