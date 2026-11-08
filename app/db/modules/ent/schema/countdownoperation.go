// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.
package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

type CountdownOperation struct{ ent.Schema }

func (CountdownOperation) Fields() []ent.Field {
	return []ent.Field{
		field.Uint64("user_id"),
		field.String("operation_id").NotEmpty().MaxLen(256),
		field.String("name").MaxLen(32),
		field.String("verb").MaxLen(8),
		field.Int64("seconds"),
		field.Bool("found"),
		field.Bool("capped"),
		field.Int64("expires_at_ms"),
		field.Int64("created_at_ms"),
	}
}

func (CountdownOperation) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("user_id", "operation_id").Unique(),
		index.Fields("created_at_ms"),
	}
}
