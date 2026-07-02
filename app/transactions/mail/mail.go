// Package mail sends the transactional emails owned by the transactions
// service through Resend. It holds no recipient state: addresses arrive from
// the users service per send and are never persisted or logged here.
package mail

import (
	"context"
	"fmt"

	"github.com/resend/resend-go/v3"
)

type Mailer struct {
	client       *resend.Client
	from         string
	dashboardURL string
}

// New builds the Resend-backed mailer. from must be a sender on a domain
// verified in Resend ("ItsBagelBot <no-reply@itsbagelbot.com>").
func New(apiKey, from, dashboardURL string) *Mailer {
	return &Mailer{
		client:       resend.NewClient(apiKey),
		from:         from,
		dashboardURL: dashboardURL,
	}
}

// SendGift emails the "you received premium" note. giftedByLogin may be
// empty (anonymous gift copy is used). idempotencyKey collapses Tebex webhook
// retries into a single send on Resend's side, mirroring the in-app
// notification's request id dedupe.
func (m *Mailer) SendGift(ctx context.Context, to, giftedByLogin, idempotencyKey string) error {

	subject := "You've been gifted a month of Premium 🥯"
	if giftedByLogin != "" {
		subject = fmt.Sprintf("%s gifted you a month of Premium 🥯", giftedByLogin)
	}

	html, err := giftHTML(giftedByLogin, m.dashboardURL)
	if err != nil {
		return fmt.Errorf("render gift email: %w", err)
	}

	_, err = m.client.Emails.SendWithOptions(ctx, &resend.SendEmailRequest{
		From:    m.from,
		To:      []string{to},
		Subject: subject,
		Html:    html,
		Text:    giftText(giftedByLogin, m.dashboardURL),
	}, &resend.SendEmailOptions{IdempotencyKey: idempotencyKey})
	if err != nil {
		return fmt.Errorf("resend send: %w", err)
	}
	return nil
}
