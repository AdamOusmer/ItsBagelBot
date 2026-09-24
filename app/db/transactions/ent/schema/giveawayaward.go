// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/dialect"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

type GiveawayAward struct{ ent.Schema }

func (GiveawayAward) Fields() []ent.Field {
	return []ent.Field{
		field.String("id").Unique().Immutable().NotEmpty(),
		field.String("giveaway_id").NotEmpty(),
		field.String("draw_id").NotEmpty(),
		field.Uint64("user_id").Positive(),
		field.Uint64("ordinal").Positive(),
		field.Int("prize_months").Positive().Immutable(),
		field.String("interval_rule").NotEmpty().Immutable(),
		field.String("state").Default("selected"),
		field.String("billing_state").Default("not_required"),
		field.String("email_state").Default("queued"),
		field.Time("planned_start").Optional().SchemaType(map[string]string{dialect.MySQL: "datetime(6)"}),
		field.Time("planned_end").Optional().SchemaType(map[string]string{dialect.MySQL: "datetime(6)"}),
		field.Time("confirmed_start").Optional().SchemaType(map[string]string{dialect.MySQL: "datetime(6)"}),
		field.Time("confirmed_end").Optional().SchemaType(map[string]string{dialect.MySQL: "datetime(6)"}),
		field.String("grant_id").Optional(),
		field.String("billing_operation_id").Optional(),
		field.String("failure_reason").Optional().MaxLen(1000),
		field.Uint64("retry_count").Default(0),
		field.Uint64("version").Default(1),
		field.Time("selected_at").Default(time.Now).Immutable(),
		field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now),
	}
}

func (GiveawayAward) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("giveaway_id", "user_id").Unique(),
		index.Fields("state"),
		index.Fields("billing_state"),
	}
}
