// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

type Automation struct {
	ent.Schema
}

func (Automation) Fields() []ent.Field {
	return []ent.Field{
		field.String("id").Unique().Immutable().NotEmpty().MaxLen(36),

		field.Uint64("user_id").Immutable(),

		field.String("name").NotEmpty().MaxLen(64),

		field.String("name_key").NotEmpty().MaxLen(256),

		field.Bool("enabled").Default(false),

		field.Int64("revision").Default(1),

		field.String("trigger_type").NotEmpty().MaxLen(32),

		field.Text("definition").NotEmpty(),

		field.Text("preview").Optional().Nillable(),

		field.Time("created_at").Default(time.Now).Immutable(),

		field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now),
	}
}

func (Automation) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("user_id"),
		index.Fields("user_id", "name_key").Unique(),
	}
}
