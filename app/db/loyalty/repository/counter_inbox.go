// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
	"time"

	"ItsBagelBot/internal/domain/event/data"
	"ItsBagelBot/pkg/codec"
	"ItsBagelBot/pkg/db"
	"github.com/google/uuid"
)

var ErrCounterReceiptMismatch = fmt.Errorf("%w: counter receipt payload mismatch", ErrInvalidInput)

func (r *Loyalty) EnsureCounterSchema(ctx context.Context) error {
	binaryID, binaryHash, binaryOwner := "VARBINARY(64)", "BINARY(32)", "BINARY(16)"
	if r.dialect == "sqlite3" {
		binaryID, binaryHash, binaryOwner = "BLOB", "BLOB", "BLOB"
	}
	query := fmt.Sprintf(`CREATE TABLE IF NOT EXISTS loyalty_counter_receipts (
		user_id BIGINT NOT NULL,
		batch_id %s NOT NULL,
		payload_hash %s NOT NULL,
		admission_id %s NOT NULL,
		created_at BIGINT NOT NULL,
		PRIMARY KEY (user_id,batch_id)
	)`, binaryID, binaryHash, binaryOwner)
	if r.dialect != "sqlite3" {
		query = strings.TrimSuffix(query, ")") + ", KEY counter_receipts_created_at (created_at)) ENGINE=InnoDB"
	}
	if _, err := r.sqldb.ExecContext(ctx, query); err != nil {
		return err
	}
	if err := r.ensureCounterReceiptIndex(ctx); err != nil {
		return err
	}
	r.counterReady.Store(true)
	return nil
}

func (r *Loyalty) ensureCounterReceiptIndex(ctx context.Context) error {
	if r.dialect != "sqlite3" {
		return nil
	}
	_, err := r.sqldb.ExecContext(ctx, "CREATE INDEX IF NOT EXISTS counter_receipts_created_at ON loyalty_counter_receipts (created_at)")
	return err
}

func counterPayloadDigest(dto data.CounterBumpedDTO) [sha256.Size]byte {
	dto.Bumps = append([]data.CounterBumpEntry(nil), dto.Bumps...)
	for i := range dto.Bumps {
		dto.Bumps[i].Scope, _ = ValidScope(dto.Bumps[i].Scope)
	}
	body, _ := codec.Marshal(dto)
	return sha256.Sum256(body)
}

type counterReceiptIdentity struct {
	user  uint64
	batch string
}

func counterIdentity(event data.CounterBumpedDTO) counterReceiptIdentity {
	digest := sha256.Sum256([]byte(event.BatchID))
	return counterReceiptIdentity{event.UserID, hex.EncodeToString(digest[:])}
}

type counterInbox struct {
	owner  uuid.UUID
	events map[counterReceiptIdentity]data.CounterBumpedDTO
	hashes map[counterReceiptIdentity][sha256.Size]byte
}

func newCounterInbox(events []data.CounterBumpedDTO) (counterInbox, error) {
	inbox := counterInbox{owner: uuid.New(), events: make(map[counterReceiptIdentity]data.CounterBumpedDTO), hashes: make(map[counterReceiptIdentity][sha256.Size]byte)}
	for _, event := range events {
		id := counterIdentity(event)
		hash := counterPayloadDigest(event)
		if prior, ok := inbox.hashes[id]; ok && prior != hash {
			return inbox, ErrCounterReceiptMismatch
		}
		inbox.events[id], inbox.hashes[id] = event, hash
	}
	return inbox, nil
}

func (b counterInbox) orderedRows() [][]any {
	ids := make([]counterReceiptIdentity, 0, len(b.events))
	for id := range b.events {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool {
		if ids[i].user != ids[j].user {
			return ids[i].user < ids[j].user
		}
		return ids[i].batch < ids[j].batch
	})
	rows := make([][]any, 0, len(ids))
	now := time.Now().Unix()
	for _, id := range ids {
		hash := b.hashes[id]
		rows = append(rows, []any{id.user, []byte(id.batch), hash[:], b.owner[:], now})
	}
	return rows
}

func (r *Loyalty) counterReceiptInserts(rows [][]any) ([]chunkStmt, error) {
	suffix := " ON DUPLICATE KEY UPDATE batch_id=batch_id"
	if r.dialect == "sqlite3" {
		suffix = " ON CONFLICT(user_id,batch_id) DO NOTHING"
	}
	spec := upsertSpec{insert: "INSERT INTO loyalty_counter_receipts (user_id,batch_id,payload_hash,admission_id,created_at) VALUES ", placeholder: "(?, ?, ?, ?, ?)", suffix: suffix}
	return counterUpsertStatements(spec, rows)
}

func (r *Loyalty) classifyCounterReceipts(ctx context.Context, tx counterSQL, inbox counterInbox, rows [][]any) error {
	chunks, err := db.StatementRowChunks(rows, 256)
	if err != nil {
		return err
	}
	for _, chunk := range chunks {
		if err := inbox.read(ctx, tx, counterReceiptRead(chunk, r.dialect != "sqlite3")); err != nil {
			return err
		}
	}
	return nil
}

func counterReceiptRead(chunk [][]any, lock bool) chunkStmt {
	predicates := make([]string, len(chunk))
	args := make([]any, 0, 2*len(chunk))
	for i, row := range chunk {
		predicates[i] = "(user_id=? AND batch_id=?)"
		args = append(args, row[0], row[1])
	}
	query := "SELECT user_id,batch_id,payload_hash,admission_id FROM loyalty_counter_receipts WHERE " + strings.Join(predicates, " OR ") + " ORDER BY user_id,batch_id"
	if lock {
		query += " FOR UPDATE"
	}
	return chunkStmt{sql: query, args: args}
}

func (b counterInbox) read(ctx context.Context, tx counterSQL, stmt chunkStmt) error {
	rows, err := tx.QueryContext(ctx, stmt.sql, stmt.args...)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		if err := b.classify(rows); err != nil {
			return err
		}
	}
	return rows.Err()
}

func (b counterInbox) classify(rows *sql.Rows) error {
	var id counterReceiptIdentity
	var hash, owner []byte
	if err := rows.Scan(&id.user, &id.batch, &hash, &owner); err != nil {
		return err
	}
	expected := b.hashes[id]
	if !bytes.Equal(expected[:], hash) {
		return ErrCounterReceiptMismatch
	}
	if !bytes.Equal(owner, b.owner[:]) {
		delete(b.events, id)
	}
	return nil
}

func (b counterInbox) admitted(events []data.CounterBumpedDTO) []data.CounterBumpedDTO {
	var admitted []data.CounterBumpedDTO
	for _, event := range events {
		id := counterIdentity(event)
		if _, ok := b.events[id]; ok {
			admitted = append(admitted, event)
			delete(b.events, id)
		}
	}
	return admitted
}
