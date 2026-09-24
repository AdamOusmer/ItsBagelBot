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

type BillingOperation struct{ ent.Schema }

func (BillingOperation) Fields() []ent.Field {
	return []ent.Field{
		field.String("id").Unique().Immutable().NotEmpty(),
		field.String("award_id").NotEmpty(),
		field.String("agreement_id").NotEmpty(),
		field.String("recurring_reference").NotEmpty(),
		field.Time("requested_start").Immutable().SchemaType(map[string]string{dialect.MySQL: "datetime(6)"}),
		field.Time("requested_end").Immutable().SchemaType(map[string]string{dialect.MySQL: "datetime(6)"}),
		field.String("state").Default("pending"),
		field.String("before_snapshot_json").Optional(),
		field.String("after_snapshot_json").Optional(),
		field.Uint64("attempts").Default(0),
		field.String("last_error").Optional().MaxLen(1000),
		field.Time("lease_until").Optional(),
		field.Uint64("version").Default(1),
		field.Time("verified_at").Optional(),
		field.Time("created_at").Default(time.Now).Immutable(),
		field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now),
	}
}

func (BillingOperation) Indexes() []ent.Index {
	return []ent.Index{index.Fields("award_id").Unique(), index.Fields("state"), index.Fields("lease_until")}
}
