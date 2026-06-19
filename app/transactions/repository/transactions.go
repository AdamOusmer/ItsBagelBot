package repository

import (
	"context"

	"ItsBagelBot/app/transactions/ent"
	"ItsBagelBot/app/transactions/ent/tebextransactions"
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
