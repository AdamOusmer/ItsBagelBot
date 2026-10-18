// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

type GiveawayInclusion struct{ ent.Schema }

func (GiveawayInclusion) Fields() []ent.Field {
	return []ent.Field{
		field.String("id").Unique().Immutable().NotEmpty(),
		field.String("giveaway_id").NotEmpty().Immutable(),
		field.Uint64("user_id").Positive().Immutable(),
		field.String("twitch_login").Optional().MaxLen(25),
		field.String("exclusion_reason").NotEmpty().MaxLen(200),
		field.Uint64("created_by").Positive(),
		field.Time("created_at").Default(time.Now).Immutable(),
	}
}

func (GiveawayInclusion) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("giveaway_id", "user_id").Unique(),
	}
}
