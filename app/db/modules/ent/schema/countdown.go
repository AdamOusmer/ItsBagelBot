// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.
package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

type Countdown struct{ ent.Schema }

func (Countdown) Fields() []ent.Field {
	return []ent.Field{
		field.Uint64("user_id").Immutable(),
		field.String("name").NotEmpty().MaxLen(128).Immutable(),
		field.Int64("expires_at_ms"),
		field.String("generation").NotEmpty().MaxLen(128),
	}
}
func (Countdown) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("user_id", "name").Unique(),
		index.Fields("expires_at_ms"),
	}
}
