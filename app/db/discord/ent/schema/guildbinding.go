// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

type GuildBinding struct {
	ent.Schema
}

func (GuildBinding) Fields() []ent.Field {
	return []ent.Field{
		field.String("guild_id").MaxLen(20).NotEmpty().Immutable(),

		field.Uint64("broadcaster_id"),

		field.String("installed_by").MaxLen(20).Optional().Default(""),

		field.Time("bound_at").Default(time.Now).Immutable(),

		field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now),
	}
}

func (GuildBinding) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("guild_id").Unique(),
		index.Fields("broadcaster_id"),
	}
}
