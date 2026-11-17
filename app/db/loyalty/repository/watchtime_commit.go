// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository

import (
	"context"
	"database/sql"
	"errors"

	"ItsBagelBot/internal/watchtime"
	"ItsBagelBot/pkg/db"
)

const watchOperationQuery = "SELECT payload_hash,completed FROM loyalty_watch_operations WHERE operation_id=?"

type watchOperation struct {
	exists    bool
	completed bool
}

func (p watchPosting) operation(row *sql.Row) (watchOperation, error) {
	var previous string
	var completed bool
	err := row.Scan(&previous, &completed)
	if errors.Is(err, sql.ErrNoRows) {
		return watchOperation{}, nil
	}
	if err != nil {
		return watchOperation{}, err
	}
	if previous != p.hash {
		return watchOperation{}, errors.New("watch operation payload mismatch")
	}
	return watchOperation{exists: true, completed: completed}, nil
}

type watchChunk struct {
	posting watchPosting
	viewers []watchViewer
	final   bool
}

func (r *Loyalty) applyWatchAward(ctx context.Context, posting watchPosting) error {
	operation, err := db.WithQuery(ctx, func(ctx context.Context) (watchOperation, error) {
		return posting.operation(r.sqldb.QueryRowContext(ctx, watchOperationQuery, watchtime.OperationID(posting.award)))
	})
	if err != nil || operation.completed {
		return err
	}
	viewers := watchViewers(posting.award)
	for len(viewers) > 0 {
		n := watchChunkSize(viewers)
		chunk := watchChunk{posting: posting, viewers: viewers[:n], final: n == len(viewers)}
		done, err := r.retryWatchChunk(ctx, chunk)
		if err != nil || done {
			return err
		}
		viewers = viewers[n:]
	}
	return nil
}

func (r *Loyalty) retryWatchChunk(ctx context.Context, chunk watchChunk) (bool, error) {
	var done bool
	err := retryTx(ctx, func(ctx context.Context) error {
		return db.WithExec(ctx, func(ctx context.Context) error {
			var err error
			done, err = r.commitWatchChunk(ctx, chunk)
			return err
		})
	})
	return done, err
}

func (r *Loyalty) commitWatchChunk(ctx context.Context, chunk watchChunk) (bool, error) {
	tx, err := r.sqldb.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	open, err := r.watchWindowOpen(ctx, tx, chunk.posting.award)
	if err != nil {
		return false, err
	}
	if !open {
		return true, chunk.posting.refused(tx.QueryRowContext(ctx, watchOperationQuery, watchtime.OperationID(chunk.posting.award)))
	}
	allowed, err := r.admitWatchAward(ctx, tx, chunk.posting.award)
	if err != nil {
		return false, err
	}
	if !allowed {
		return true, chunk.posting.commitRefused(ctx, tx)
	}
	done, err := r.applyWatchChunk(ctx, tx, chunk)
	if err != nil {
		return false, err
	}
	return done, tx.Commit()
}

func (r *Loyalty) applyWatchChunk(ctx context.Context, tx *sql.Tx, chunk watchChunk) (bool, error) {
	posting := chunk.posting
	completed, err := r.prepareWatchOperation(ctx, tx, posting)
	if err != nil || completed {
		return true, err
	}
	if err := r.postWatchChunk(ctx, tx, posting, chunk.viewers); err != nil {
		return false, err
	}
	if !chunk.final {
		return false, nil
	}
	return true, posting.complete(ctx, tx)
}

func (p watchPosting) commitRefused(ctx context.Context, tx *sql.Tx) error {
	operation, err := p.operation(tx.QueryRowContext(ctx, watchOperationQuery, watchtime.OperationID(p.award)))
	if err != nil {
		return err
	}
	if !operation.exists {
		return tx.Commit()
	}
	if err := p.complete(ctx, tx); err != nil {
		return err
	}
	return tx.Commit()
}

func (p watchPosting) complete(ctx context.Context, tx *sql.Tx) error {
	_, err := tx.ExecContext(ctx, "UPDATE loyalty_watch_operations SET completed=1 WHERE operation_id=?", watchtime.OperationID(p.award))
	return err
}

func (r *Loyalty) prepareWatchOperation(ctx context.Context, tx *sql.Tx, posting watchPosting) (bool, error) {
	operation, err := posting.operation(tx.QueryRowContext(ctx, watchOperationQuery, watchtime.OperationID(posting.award)))
	if err != nil || operation.exists {
		return operation.completed, err
	}
	return false, r.startWatchOperation(ctx, tx, posting)
}

func (r *Loyalty) startWatchOperation(ctx context.Context, tx *sql.Tx, posting watchPosting) error {
	if err := r.preflightWatchBalances(ctx, tx, posting); err != nil {
		return err
	}
	a := posting.award
	_, err := tx.ExecContext(ctx, "INSERT INTO loyalty_watch_operations (operation_id,payload_hash,user_id,created_at,window_started_at_ms,completed) VALUES (?, ?, ?, ?, ?, 0)", watchtime.OperationID(a), posting.hash, a.UserID, posting.postedAt.Unix(), watchtime.WindowStartUnixMilli(a))
	return err
}

func (p watchPosting) refused(row *sql.Row) error {
	operation, err := p.operation(row)
	if err != nil {
		return err
	}
	if operation.exists && !operation.completed {
		return errors.New("watch window finalized during incomplete award")
	}
	return nil
}
