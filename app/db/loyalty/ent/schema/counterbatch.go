// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	entschema "entgo.io/ent/schema"
	"entgo.io/ent/schema/field"
	"time"
)

// CounterBatch is a permanent receipt for an applied producer window. Removing
// receipts permits old JetStream deliveries to be applied again.
type CounterBatch struct{ ent.Schema }

func (CounterBatch) Fields() []ent.Field {
	return []ent.Field{
		field.String("id").MaxLen(255).Immutable(),
		field.Time("created_at").Default(time.Now).Immutable(),
	}
}

func (CounterBatch) Annotations() []entschema.Annotation {
	return []entschema.Annotation{entsql.Annotation{Collation: "utf8mb4_bin"}}
}
