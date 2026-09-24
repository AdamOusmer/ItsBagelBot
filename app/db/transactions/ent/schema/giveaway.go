// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

type Giveaway struct{ ent.Schema }

func (Giveaway) Fields() []ent.Field {
	return []ent.Field{
		field.String("id").Unique().Immutable().NotEmpty(),
		field.String("idempotency_key").Unique().Immutable().NotEmpty(),
		field.String("title").NotEmpty().MaxLen(200),
		field.String("reason").Optional().MaxLen(1000),
		field.String("rules_version").NotEmpty(),
		field.Int("winner_count").Positive(),
		field.Int("prize_months").Positive(),
		field.String("status").Default("draft").Comment("draft, frozen, drawn, cancelled"),
		field.Uint64("created_by").Positive(),
		field.Uint64("version").Default(1),
		field.Time("created_at").Default(time.Now).Immutable(),
		field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now),
		field.Time("frozen_at").Optional(),
		field.Time("drawn_at").Optional(),
		field.String("freeze_idempotency_key").Optional(),
		field.String("frozen_pool_digest").Optional(),
	}
}

func (Giveaway) Indexes() []ent.Index {
	return []ent.Index{index.Fields("status"), index.Fields("created_at")}
}
