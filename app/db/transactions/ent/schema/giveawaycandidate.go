// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

type GiveawayCandidate struct{ ent.Schema }

func (GiveawayCandidate) Fields() []ent.Field {
	return []ent.Field{
		field.String("id").Unique().Immutable().NotEmpty(),
		field.String("giveaway_id").NotEmpty(),
		field.Uint64("user_id").Positive(),
		field.String("twitch_login").Optional().MaxLen(25),
		field.String("eligibility_json").NotEmpty(),
		field.String("exclusion_reason").Optional().MaxLen(200),
		field.Bool("eligible").Default(true),
		field.String("pool_digest").NotEmpty(),
		field.Time("created_at").Default(time.Now).Immutable(),
	}
}

func (GiveawayCandidate) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("giveaway_id", "user_id").Unique(),
		index.Fields("giveaway_id", "eligible"),
	}
}
