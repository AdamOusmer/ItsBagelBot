// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

type Balance struct {
	ent.Schema
}

func (Balance) Fields() []ent.Field {
	return []ent.Field{
		field.Uint64("user_id").Immutable(),

		field.Uint64("viewer_id").Immutable(),

		field.String("viewer_login").Optional().MaxLen(64),
		field.String("viewer_name").Optional().MaxLen(64),

		field.Int64("points").Default(0),

		field.Uint64("watch_seconds").Default(0),

		field.Time("created_at").Default(time.Now),

		field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now),
	}
}

func (Balance) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("user_id", "viewer_id").
			Unique(),
		index.Fields("user_id", "points"),
	}
}
