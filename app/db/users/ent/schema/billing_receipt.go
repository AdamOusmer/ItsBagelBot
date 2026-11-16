// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	entschema "entgo.io/ent/schema"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

type BillingReceipt struct{ ent.Schema }

func (BillingReceipt) Fields() []ent.Field {
	return []ent.Field{
		field.String("id").StorageKey("event_id").NotEmpty().Immutable(),
		field.Time("created_at").Default(time.Now).Immutable(),
	}
}

func (BillingReceipt) Indexes() []ent.Index {
	return []ent.Index{index.Fields("created_at")}
}

func (BillingReceipt) Annotations() []entschema.Annotation {
	return []entschema.Annotation{entsql.Annotation{Collation: "utf8mb4_bin"}}
}
