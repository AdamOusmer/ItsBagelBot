// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.
package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

type CountdownDelivery struct{ ent.Schema }

func (CountdownDelivery) Fields() []ent.Field {
	return []ent.Field{
		field.Uint64("user_id"),
		field.String("name").MaxLen(32),
		field.String("generation").NotEmpty().MaxLen(128),
		field.Int64("expires_at_ms"),
		field.String("lease_token").Default("").MaxLen(128),
		field.Int64("lease_until_ms").Default(0),
		field.JSON("progress", map[string]string{}),
	}
}

func (CountdownDelivery) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("generation").Unique(),
		index.Fields("lease_until_ms"),
	}
}
