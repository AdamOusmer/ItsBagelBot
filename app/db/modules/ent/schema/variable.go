// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

type Variable struct {
	ent.Schema
}

func (Variable) Fields() []ent.Field {
	return []ent.Field{
		field.Uint64("user_id").Immutable(),

		field.String("scope").MaxLen(16).Immutable(),

		field.Uint64("viewer_id").Default(0).Immutable(),

		field.String("name").NotEmpty().MaxLen(32).Immutable(),

		// Ent's MaxLen counts bytes; the repository enforces MaxVariableValueRunes.
		field.String("value").MaxLen(2000).Default(""),

		field.Int64("revision").Default(0),

		field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now),
	}
}

func (Variable) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("user_id", "scope", "viewer_id", "name").Unique(),
		index.Fields("user_id", "scope", "name"),
	}
}
