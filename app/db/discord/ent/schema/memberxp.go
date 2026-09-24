// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

type MemberXP struct {
	ent.Schema
}

func (MemberXP) Fields() []ent.Field {
	return []ent.Field{
		field.String("guild_id").MaxLen(20).NotEmpty().Immutable(),

		field.String("user_id").MaxLen(20).NotEmpty().Immutable(),

		field.Int64("xp").Default(0),

		field.Int("level").Default(0),

		field.Time("last_daily").Optional().Nillable(),

		field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now),
	}
}

func (MemberXP) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("guild_id", "user_id").Unique(),
		index.Fields("guild_id", "xp"),
	}
}
