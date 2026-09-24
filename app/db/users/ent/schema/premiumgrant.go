// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/dialect"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

type PremiumGrant struct {
	ent.Schema
}

func (PremiumGrant) Fields() []ent.Field {
	return []ent.Field{
		field.Uint64("user_id"),
		field.String("giveaway_id").NotEmpty().MaxLen(128),
		field.String("award_id").NotEmpty().MaxLen(128),
		field.Enum("state").Values("prepared", "committed", "cancelled", "expired").Default("prepared"),
		field.Enum("projection_phase").Values("pending", "active", "expired").Default("pending"),
		field.Time("start_at").SchemaType(map[string]string{dialect.MySQL: "datetime(6)"}),
		field.Time("end_at").SchemaType(map[string]string{dialect.MySQL: "datetime(6)"}),
		field.String("interval_rule_version").NotEmpty().MaxLen(128),
		field.Time("created_at").Default(time.Now),
		field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now),
	}
}

func (PremiumGrant) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("user", User.Type).Ref("premium_grants").Field("user_id").Unique().Required(),
	}
}

func (PremiumGrant) Indexes() []ent.Index {
	return []ent.Index{
		// Must not include user_id, or a retry could deliver one award to two accounts.
		index.Fields("giveaway_id", "award_id").Unique(),
		index.Fields("user_id", "state", "start_at", "end_at"),
	}
}
