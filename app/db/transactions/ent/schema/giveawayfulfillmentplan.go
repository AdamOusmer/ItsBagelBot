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

// GiveawayFulfillmentPlan freezes the exact interval and policy used for one
// award. It is immutable so retries cannot recalculate a different prize.
type GiveawayFulfillmentPlan struct{ ent.Schema }

func (GiveawayFulfillmentPlan) Fields() []ent.Field {
	return []ent.Field{
		field.String("id").Unique().Immutable().NotEmpty(),
		field.String("award_id").Immutable().NotEmpty(),
		field.String("interval_rule").Immutable().NotEmpty(),
		field.Time("start_at").Immutable().SchemaType(map[string]string{dialect.MySQL: "datetime(6)"}),
		field.Time("end_at").Immutable().SchemaType(map[string]string{dialect.MySQL: "datetime(6)"}),
		field.Time("created_at").Immutable().Default(time.Now),
	}
}

func (GiveawayFulfillmentPlan) Indexes() []ent.Index {
	return []ent.Index{index.Fields("award_id").Unique()}
}
