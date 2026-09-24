// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

type Quote struct {
	ent.Schema
}

func (Quote) Fields() []ent.Field {
	return []ent.Field{
		field.Uint64("user_id").Immutable(),

		field.Uint64("number").Immutable(),

		field.String("text").NotEmpty().MaxLen(450),

		field.String("added_by").Default("").MaxLen(64),

		field.Time("created_at").Default(time.Now),
	}
}

func (Quote) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("user_id", "number").
			Unique(),
	}
}
