package repository

import (
	"context"
	"errors"
	"strings"
	"time"

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

// TransactionProof is the audit evidence that a verified Tebex webhook applied a
// transaction: the webhook that carried it, its type, the entitled user, and
// when it was recorded. It is read straight from the webhook audit log; there is
// no separate transaction store (Tebex stays the payment system of record).
type TransactionProof struct {
	TransactionID string
	UserID        uint64
	WebhookID     string
	EventType     string
	ProcessedAt   time.Time
}

// ProcessedProof returns the audit evidence that a given Tebex transaction was
// processed: the earliest webhook event with status "processed" carrying that
// transaction id. ok is false when the audit holds no processed row for it. This
// is the proof-of-processing lookup over the existing audit; it adds no storage.
func (r *Transactions) ProcessedProof(ctx context.Context, transactionID string) (TransactionProof, bool, error) {

	if transactionID == "" {
		return TransactionProof{}, false, errors.New("transaction id required")
	}

	row, err := db.WithQuery(ctx, func(ctx context.Context) (*ent.TebexWebhookEvents, error) {
		return r.client.TebexWebhookEvents.Query().
			Where(
				tebexwebhookevents.TransactionIDEQ(transactionID),
				tebexwebhookevents.StatusEQ(tebexwebhookevents.StatusProcessed),
			).
			Order(ent.Asc(tebexwebhookevents.FieldCreatedAt)).
			First(ctx)
	})
	if ent.IsNotFound(err) {
		return TransactionProof{}, false, nil
	}
	if err != nil {
		return TransactionProof{}, false, err
	}

	return TransactionProof{
		TransactionID: row.TransactionID,
		UserID:        row.UserID,
		WebhookID:     row.ID,
		EventType:     row.EventType,
		ProcessedAt:   row.CreatedAt,
	}, true, nil
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
