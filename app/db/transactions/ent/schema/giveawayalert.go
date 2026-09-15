// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// GiveawayAlert remains visible until the underlying problem is resolved.
type GiveawayAlert struct{ ent.Schema }

func (GiveawayAlert) Fields() []ent.Field {
	return []ent.Field{
		field.String("id").Unique().Immutable().NotEmpty(),
		field.String("award_id").NotEmpty(),
		field.String("operation_id").Optional(),
		field.String("category").NotEmpty(),
		field.String("state").Default("unresolved"),
		field.String("message").NotEmpty().MaxLen(2000),
		field.Time("affected_boundary").Optional(),
		field.Time("first_seen_at").Default(time.Now).Immutable(),
		field.Time("last_seen_at").Default(time.Now).UpdateDefault(time.Now),
		field.Uint64("acknowledged_by").Optional(),
		field.Time("acknowledged_at").Optional(),
		field.Time("resolved_at").Optional(),
	}
}

func (GiveawayAlert) Indexes() []ent.Index {
	return []ent.Index{index.Fields("award_id", "category").Unique(), index.Fields("state"), index.Fields("affected_boundary")}
}
