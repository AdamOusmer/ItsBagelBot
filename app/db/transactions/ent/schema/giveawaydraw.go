// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

type GiveawayDraw struct{ ent.Schema }

func (GiveawayDraw) Fields() []ent.Field {
	return []ent.Field{
		field.String("id").Unique().Immutable().NotEmpty(),
		field.String("giveaway_id").NotEmpty(),
		field.String("operation_key").NotEmpty(),
		field.String("pool_digest").NotEmpty(),
		field.String("algorithm_version").NotEmpty(),
		field.String("audit_json").NotEmpty(),
		field.String("winner_ids_json").NotEmpty(),
		field.Uint64("actor_id").Positive(),
		field.Time("created_at").Default(time.Now).Immutable(),
	}
}

func (GiveawayDraw) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("giveaway_id").Unique(),
		index.Fields("operation_key").Unique(),
	}
}
