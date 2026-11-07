// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository

import (
	"errors"

	"entgo.io/ent/dialect"

	"ItsBagelBot/app/db/discord/ent"
)

var (
	ErrBoundElsewhere  = errors.New("discord: already bound to a different broadcaster")
	ErrNotBound        = errors.New("discord: guild is not bound")
	ErrOpenLimit       = errors.New("discord: ticket open limit reached")
	ErrNotFound        = errors.New("discord: not found")
	ErrInvalidInput    = errors.New("discord: invalid input")
	ErrVersionConflict = errors.New("discord: config version conflict")
)

type Store struct {
	client   *ent.Client
	rowLocks bool
}

func New(client *ent.Client, dialectName string) *Store {
	return &Store{client: client, rowLocks: dialectName == dialect.MySQL}
}
