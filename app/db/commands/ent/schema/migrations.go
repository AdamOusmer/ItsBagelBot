// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/field"
)

// Migrations is a marker table for one-time data backfills run at boot
// (repository.BackfillBumpCounterFromTokens is the first), as distinct from
// AutoMigrate's schema DDL: a broadcaster-editable ROW value (bump_counter)
// cannot be derived from a schema shape, so it needs its own once-only gate
// that a plain `client.Schema.Create` does not give. One row per applied
// migration; a name absent from this table has never run.
//
// This lives beside the service's own schema rather than as a shared
// pkg/svcboot facility because every service so far has needed this exactly
// once (commands) — a shared migrations table would be speculative
// generality for a problem with one example.
type Migrations struct {
	ent.Schema
}

func (Migrations) Fields() []ent.Field {
	return []ent.Field{
		field.String("name").NotEmpty().Unique(),
		field.Time("applied_at").Default(time.Now),
	}
}
