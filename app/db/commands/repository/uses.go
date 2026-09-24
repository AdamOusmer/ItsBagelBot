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

// RecordUse commits a producer batch and its receipt atomically. A retry after an
// ambiguous commit cannot apply the same batch twice. Missing commands are
// consumed without recreation, preserving deletion behavior.
func (r *Commands) RecordUse(ctx context.Context, batchID string, userID uint64, name string, count int64) error {
	name = normalizeName(name)
	if err := validate.UserID(userID); err != nil {
		return err
	}
	if err := validate.CommandName(name); err != nil {
		return err
	}
	if batchID == "" || len(batchID) > 128 {
		return fmt.Errorf("command use batch ID must contain 1..128 bytes")
	}
	if count == 0 {
		count = 1
	} // legacy event semantics
	if count < 0 {
		return fmt.Errorf("command use delta must be nonnegative")
	}
	delta := count
	err := db.WithExec(ctx, func(ctx context.Context) error {
		tx, err := r.client.Tx(ctx)
		if err != nil {
			return err
		}
		defer func() { _ = tx.Rollback() }()
		receipt, err := tx.CommandUseBatch.Get(ctx, batchID)
		if err == nil {
			if receipt.UserID != userID || receipt.Name != name || receipt.Count != delta {
				return fmt.Errorf("command use batch ID reused with different payload")
			}
			return nil
		}
		if !ent.IsNotFound(err) {
			return err
		}
		if err := tx.CommandUseBatch.Create().SetID(batchID).SetUserID(userID).SetName(name).SetCount(delta).Exec(ctx); err != nil {
			return err
		}
		// Guard before addition so SQLite cannot promote an overflowing integer
		// to REAL and MySQL cannot overflow BIGINT. The predicate is atomic.
		updated, err := tx.Commands.Update().Where(commands.UserIDEQ(userID), commands.NameEQ(name), commands.UsesLTE(data.MaxCounter-delta)).AddUses(delta).Save(ctx)
		if err != nil {
			return err
		}
		if updated == 0 {
			exists, err := tx.Commands.Query().Where(commands.UserIDEQ(userID), commands.NameEQ(name)).Exist(ctx)
			if err != nil {
				return err
			}
			if exists {
				return fmt.Errorf("command use total exceeds signed int64 range")
			}
		}
		return tx.Commit()
	})
	if err != nil {
		return err
	}
	txn := newrelic.FromContext(ctx)
	r.publishUseEvents(ctx, txn, []commandKey{{userID: userID, name: name}})
	return nil
}
