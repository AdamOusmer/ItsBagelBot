package repository

import (
	"context"
	"errors"
	"strings"

	"ItsBagelBot/app/transactions/ent"
	"ItsBagelBot/app/transactions/ent/tebexwebhookevents"
	"ItsBagelBot/pkg/db"
)

// Transactions keeps the Tebex webhook audit log and nothing else. Tebex is
// the merchant of record, so payment and transaction records stay on their
// side; the audit rows (webhook id, type, status, transaction id, user id)
// are what we need for entitlement forensics. This is the money path: every
// write goes straight to the database, no caching, no batching.
type Transactions struct {
	client *ent.Client
}

type WebhookStatus string

const (
	WebhookProcessed  WebhookStatus = "processed"
	WebhookFailed     WebhookStatus = "failed"
	WebhookIgnored    WebhookStatus = "ignored"
	WebhookValidation WebhookStatus = "validation"
)

type WebhookEvent struct {
	ID            string
	Type          string
	Status        WebhookStatus
	TransactionID string
	UserID        uint64
	Error         string
}

func NewTransactions(client *ent.Client) *Transactions {
	return &Transactions{
		client: client,
	}
}

func (r *Transactions) SaveWebhookEvent(ctx context.Context, event WebhookEvent) error {

	if event.ID == "" {
		return errors.New("webhook id required")
	}
	if event.Type == "" {
		return errors.New("webhook type required")
	}

	status, err := webhookStatus(event.Status)
	if err != nil {
		return err
	}

	event.Error = truncateWebhookError(event.Error)

	err = db.WithExec(ctx, func(ctx context.Context) error {
		create := r.client.TebexWebhookEvents.Create().
			SetID(event.ID).
			SetEventType(event.Type).
			SetStatus(status)

		if event.TransactionID != "" {
			create.SetTransactionID(event.TransactionID)
		}
		if event.UserID != 0 {
			create.SetUserID(event.UserID)
		}
		if event.Error != "" {
			create.SetError(event.Error)
		}

		return create.Exec(ctx)
	})

	if err == nil {
		return nil
	}
	if !ent.IsConstraintError(err) {
		return err
	}

	return db.WithExec(ctx, func(ctx context.Context) error {
		update := r.client.TebexWebhookEvents.UpdateOneID(event.ID).
			SetStatus(status)

		if event.TransactionID != "" {
			update.SetTransactionID(event.TransactionID)
		} else {
			update.ClearTransactionID()
		}
		if event.UserID != 0 {
			update.SetUserID(event.UserID)
		} else {
			update.ClearUserID()
		}
		if event.Error != "" {
			update.SetError(event.Error)
		} else {
			update.ClearError()
		}

		return update.Exec(ctx)
	})
}

func webhookStatus(status WebhookStatus) (tebexwebhookevents.Status, error) {

	switch status {
	case WebhookProcessed:
		return tebexwebhookevents.StatusProcessed, nil
	case WebhookFailed:
		return tebexwebhookevents.StatusFailed, nil
	case WebhookIgnored:
		return tebexwebhookevents.StatusIgnored, nil
	case WebhookValidation:
		return tebexwebhookevents.StatusValidation, nil
	default:
		return "", errors.New("invalid webhook status")
	}
}

func truncateWebhookError(msg string) string {

	msg = strings.TrimSpace(msg)
	if len(msg) <= 500 {
		return msg
	}

	return msg[:500]
}
