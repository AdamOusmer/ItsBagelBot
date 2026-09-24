// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

type CounterEntry struct {
	ent.Schema
}

func (CounterEntry) Fields() []ent.Field {
	return []ent.Field{
		field.Uint64("user_id").Immutable(),

		field.String("name").NotEmpty().MaxLen(64),

		field.String("command").Default("").MaxLen(64).Immutable(),

		field.Uint64("viewer_id").Immutable(),

		field.String("viewer_login").Optional().MaxLen(64),
		field.String("viewer_name").Optional().MaxLen(64),

		field.Int64("value").Default(0),

		field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now),
	}
}

func (CounterEntry) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("user_id", "name", "command", "viewer_id").
			Unique(),
	}
}

func (CounterEntry) Hooks() []ent.Hook {
	return []ent.Hook{
		normalizeNameHook(),
	}
}
