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

type TebexAgreement struct{ ent.Schema }

func (TebexAgreement) Fields() []ent.Field {
	return []ent.Field{
		field.String("id").Unique().Immutable().NotEmpty(),
		field.Uint64("user_id").Positive(),
		field.String("store_id").NotEmpty(),
		field.String("recurring_reference").NotEmpty(),
		field.String("interval").NotEmpty(),
		field.String("provider_status").NotEmpty(),
		field.Bool("cancel_requested").Default(false),
		field.Time("next_collection_at").Optional().SchemaType(map[string]string{dialect.MySQL: "datetime(6)"}),
		field.Time("paid_through_at").Optional().SchemaType(map[string]string{dialect.MySQL: "datetime(6)"}),
		field.String("paid_through_source").Optional(),
		field.Time("paused_until").Optional().SchemaType(map[string]string{dialect.MySQL: "datetime(6)"}),
		field.String("last_snapshot_json").Optional(),
		field.Time("verified_at").Optional(),
		field.Time("created_at").Default(time.Now).Immutable(),
		field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now),
	}
}

func (TebexAgreement) Indexes() []ent.Index {
	return []ent.Index{index.Fields("recurring_reference").Unique(), index.Fields("user_id"), index.Fields("provider_status")}
}
