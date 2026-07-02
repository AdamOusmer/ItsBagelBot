package repository

import (
	"context"
	"errors"
	"strings"

	"ItsBagelBot/app/transactions/ent"
	"ItsBagelBot/app/transactions/ent/tebextransactions"
	"ItsBagelBot/app/transactions/ent/tebexwebhookevents"
	"ItsBagelBot/internal/domain/event/data"
	"ItsBagelBot/internal/domain/validate"
	"ItsBagelBot/pkg/bus"
	"ItsBagelBot/pkg/db"

	"github.com/ThreeDotsLabs/watermill/message"
)

// Transactions records which Tebex transaction belongs to which user, and
// nothing else; payment details stay on Tebex's side. This is the money path:
// every write goes straight to the database, no caching, no batching.
type Transactions struct {
	client *ent.Client
	pub    message.Publisher
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

func NewTransactions(client *ent.Client, pub message.Publisher) *Transactions {
	return &Transactions{
		client: client,
		pub:    pub,
	}
}

// Record stores the transaction. Tebex webhooks retry, so a duplicate ID is
// treated as already recorded rather than as an error, which keeps the
// handler idempotent without a read-before-write.
func (r *Transactions) Record(ctx context.Context, id string, userID uint64) error {

	if err := validate.TransactionID(id); err != nil {
		return err
	}
	if err := validate.UserID(userID); err != nil {
		return err
	}

	err := db.WithExec(ctx, func(ctx context.Context) error {
		return r.client.TebexTransactions.Create().
			SetID(id).
			SetUserID(userID).
			Exec(ctx)
	})

	if err != nil {
		if ent.IsConstraintError(err) {
			return nil // webhook retry, already recorded
		}
		return err
	}

	return bus.PublishJSON(ctx, r.pub, data.SubjectTransactionRecorded, data.TransactionRecordedDTO{
		ID:     id,
		UserID: userID,
	})
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

// UserOf returns the owner of a transaction.
func (r *Transactions) UserOf(ctx context.Context, id string) (uint64, error) {

	row, err := db.WithQuery(ctx, func(ctx context.Context) (*ent.TebexTransactions, error) {
		return r.client.TebexTransactions.Query().
			Where(tebextransactions.IDEQ(id)).
			Only(ctx)
	})
	if err != nil {
		return 0, err
	}

	return row.UserID, nil
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
