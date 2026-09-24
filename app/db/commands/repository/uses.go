package repository

import (
	"context"
	"fmt"

	"ItsBagelBot/app/db/commands/ent"
	"ItsBagelBot/app/db/commands/ent/commands"
	"ItsBagelBot/internal/domain/event/data"
	"ItsBagelBot/internal/domain/validate"
	"ItsBagelBot/pkg/db"
	"github.com/newrelic/go-agent/v3/newrelic"
)

type commandUseBatch struct {
	id     string
	userID uint64
	name   string
	count  int64
}

// RecordUse commits a producer batch and its receipt atomically. A retry after an
// ambiguous commit cannot apply the same batch twice. Missing commands are
// consumed without recreation, preserving deletion behavior.
func (r *Commands) RecordUse(ctx context.Context, batchID string, dto data.CommandUsedDTO) error {
	batch := commandUseBatch{id: batchID, userID: dto.UserID, name: normalizeName(dto.Name), count: dto.Count}
	if batch.count == 0 {
		batch.count = 1 // legacy event semantics
	}
	if err := batch.validate(); err != nil {
		return err
	}
	if err := db.WithExec(ctx, func(ctx context.Context) error {
		return r.commitUseBatch(ctx, batch)
	}); err != nil {
		return err
	}

	txn := newrelic.FromContext(ctx)
	r.publishUseEvents(ctx, txn, []commandKey{{userID: batch.userID, name: batch.name}})
	return nil
}

func (b commandUseBatch) validate() error {
	if err := validate.UserID(b.userID); err != nil {
		return err
	}
	if err := validate.CommandName(b.name); err != nil {
		return err
	}
	if b.id == "" || len(b.id) > 128 {
		return fmt.Errorf("command use batch ID must contain 1..128 bytes")
	}
	if b.count < 0 {
		return fmt.Errorf("command use delta must be nonnegative")
	}
	return nil
}

func (r *Commands) commitUseBatch(ctx context.Context, batch commandUseBatch) error {
	tx, err := r.client.Tx(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	seen, err := batch.receiptExists(ctx, tx)
	if err != nil || seen {
		return err
	}
	if err := tx.CommandUseBatch.Create().SetID(batch.id).SetUserID(batch.userID).SetName(batch.name).SetCount(batch.count).Exec(ctx); err != nil {
		return err
	}
	if err := batch.apply(ctx, tx); err != nil {
		return err
	}
	return tx.Commit()
}

func (b commandUseBatch) receiptExists(ctx context.Context, tx *ent.Tx) (bool, error) {
	receipt, err := tx.CommandUseBatch.Get(ctx, b.id)
	if ent.IsNotFound(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	recorded := commandUseBatch{id: receipt.ID, userID: receipt.UserID, name: receipt.Name, count: receipt.Count}
	if recorded != b {
		return false, fmt.Errorf("command use batch ID reused with different payload")
	}
	return true, nil
}

func (b commandUseBatch) apply(ctx context.Context, tx *ent.Tx) error {
	// Guard before addition so SQLite cannot promote an overflowing integer
	// to REAL and MySQL cannot overflow BIGINT. The predicate is atomic.
	updated, err := tx.Commands.Update().Where(commands.UserIDEQ(b.userID), commands.NameEQ(b.name), commands.UsesLTE(data.MaxCounter-b.count)).AddUses(b.count).Save(ctx)
	if err != nil || updated != 0 {
		return err
	}
	return b.checkMissingCommand(ctx, tx)
}

func (b commandUseBatch) checkMissingCommand(ctx context.Context, tx *ent.Tx) error {
	exists, err := tx.Commands.Query().Where(commands.UserIDEQ(b.userID), commands.NameEQ(b.name)).Exist(ctx)
	if err != nil {
		return err
	}
	if exists {
		return fmt.Errorf("command use total exceeds signed int64 range")
	}
	return nil
}
