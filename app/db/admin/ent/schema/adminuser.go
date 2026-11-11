// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

type AdminUser struct {
	ent.Schema
}

func (AdminUser) Fields() []ent.Field {
	return []ent.Field{
		field.Uint64("id").Unique().Immutable(),

		field.String("login").NotEmpty(),

		field.String("display_name").NotEmpty(),

		field.Enum("role").
			Values("moderator", "admin", "owner").
			Default("moderator"),

		field.Bool("active").Default(true),

		field.Uint64("added_by").Default(0),

		field.Time("created_at").Default(time.Now),

		field.Time("updated_at").
			Default(time.Now).
			UpdateDefault(time.Now),
	}
}

func (AdminUser) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("active"),
		index.Fields("role", "active"),
	}
}
