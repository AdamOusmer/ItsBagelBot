// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository

import (
	"bytes"
	"context"
	"database/sql"

	"ItsBagelBot/pkg/db"
)

func (p *CounterProcessor) checkCompletedCounterReceipts(ctx context.Context, tracked []*counterDelivery) {
	var completed []*counterDelivery
	var rows [][]any
	for _, delivery := range tracked {
		if !delivery.completed {
			continue
		}
		completed = append(completed, delivery)
		id := counterIdentity(delivery.request.dto)
		rows = append(rows, []any{id.user, []byte(id.batch)})
	}
	hashes, err := p.repo.cachedCounterDigests(ctx, rows)
	for _, delivery := range completed {
		delivery.err = completedCounterError(delivery, hashes, err)
	}
}

func completedCounterError(delivery *counterDelivery, hashes map[counterReceiptIdentity][]byte, err error) error {
	if err != nil {
		return err
	}
	stored, exists := hashes[counterIdentity(delivery.request.dto)]
	if !exists {
		return nil
	}
	digest := counterPayloadDigest(delivery.request.dto)
	if !bytes.Equal(stored, digest[:]) {
		return ErrCounterReceiptMismatch
	}
	return nil
}

func (r *Loyalty) cachedCounterDigests(ctx context.Context, rows [][]any) (map[counterReceiptIdentity][]byte, error) {
	return db.WithQuery(ctx, func(ctx context.Context) (map[counterReceiptIdentity][]byte, error) {
		hashes := make(map[counterReceiptIdentity][]byte)
		chunks, err := db.StatementRowChunks(rows, 256)
		if err != nil {
			return nil, err
		}
		for _, chunk := range chunks {
			stmt := counterReceiptRead(chunk, false)
			if err := r.readCachedCounterDigests(ctx, stmt, hashes); err != nil {
				return nil, err
			}
		}
		return hashes, nil
	})
}

func (r *Loyalty) readCachedCounterDigests(ctx context.Context, stmt chunkStmt, hashes map[counterReceiptIdentity][]byte) error {
	rows, err := r.sqldb.QueryContext(ctx, stmt.sql, stmt.args...)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		if err := scanCachedCounterDigest(rows, hashes); err != nil {
			return err
		}
	}
	return rows.Err()
}

func scanCachedCounterDigest(rows *sql.Rows, hashes map[counterReceiptIdentity][]byte) error {
	var id counterReceiptIdentity
	var hash, owner []byte
	if err := rows.Scan(&id.user, &id.batch, &hash, &owner); err != nil {
		return err
	}
	hashes[id] = hash
	return nil
}
