// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// Package repository is discord-data's data access layer: one type wrapping
// the generated ent client, with the guild-binding, ticket-desk and member-XP
// verbs as methods on it. The RPC layer declares the narrow interfaces it
// needs (consumer side) and this package satisfies them, so nothing here has
// to know what a NATS subject is.
package repository

import (
	"context"
	"errors"

	"entgo.io/ent/dialect"

	"ItsBagelBot/app/db/discord/ent"
)

// Sentinel errors the RPC layer maps onto reply codes. Anything else is an
// internal failure and travels as its own message.
var (
	// ErrBoundElsewhere: the guild is already bound to a DIFFERENT
	// broadcaster. Re-binding a guild to the broadcaster it already belongs to
	// is idempotent, and a broadcaster may own any number of guilds, so this
	// is the one binding refusal left.
	ErrBoundElsewhere = errors.New("discord: already bound to a different broadcaster")
	// ErrNotBound: no binding row exists for that guild.
	ErrNotBound = errors.New("discord: guild is not bound")
	// ErrOpenLimit: the opener already holds the guild's maximum open tickets.
	ErrOpenLimit = errors.New("discord: ticket open limit reached")
	// ErrNotFound: the addressed ticket or transcript does not exist.
	ErrNotFound = errors.New("discord: not found")
	// ErrInvalidInput: the request itself is malformed or names an impossible
	// transition (claiming a closed ticket, an empty guild id).
	ErrInvalidInput = errors.New("discord: invalid input")
	// ErrVersionConflict: the settings row moved on since the caller read it.
	ErrVersionConflict = errors.New("discord: config version conflict")
)

// Store is the service's whole data access surface. One type rather than one
// per table because the three keyspaces share a transaction helper and a
// dialect flag, and splitting them would mean threading both through three
// constructors for no isolation anyone benefits from.
type Store struct {
	client *ent.Client
	// rowLocks says whether SELECT ... FOR UPDATE is available. MySQL has it
	// and the read-modify-write paths (ticket open under a limit, XP add,
	// daily window) take it to serialize concurrent writers on one row.
	// SQLite -- the enttest dialect -- rejects the clause outright
	// (entgo.io/ent dialect/sql: "SELECT .. FOR UPDATE/SHARE not supported in
	// SQLite"), and does not need it: it takes a file-wide write lock for the
	// whole transaction, which is strictly stronger. Hence a flag rather than
	// dropping the lock everywhere or keeping the tests off the real paths.
	rowLocks bool
}

// New builds the store. dialectName is the driver's dialect
// (entgo.io/ent/dialect.MySQL in production, dialect.SQLite under enttest);
// it decides only whether the read-modify-write paths take row locks.
func New(client *ent.Client, dialectName string) *Store {
	return &Store{client: client, rowLocks: dialectName == dialect.MySQL}
}

// withTx runs fn inside one ent transaction, committing on success and rolling
// back on error. Copied per service by house rule (loyalty has the twin);
// deliberately unexported so no caller can start a transaction from outside.
func withTx(ctx context.Context, client *ent.Client, fn func(tx *ent.Tx) error) error {
	tx, err := client.Tx(ctx)
	if err != nil {
		return err
	}
	if err := fn(tx); err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
}
