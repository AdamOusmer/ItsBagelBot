// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

type CooldownClaim struct {
	ent.Schema
}

func (CooldownClaim) Fields() []ent.Field {
	return []ent.Field{
		field.Uint64("user_id").Immutable(),

		field.String("cooldown_key").NotEmpty().MaxLen(128).Immutable(),

		field.Int64("expires_at_ms"),
	}
}

func (CooldownClaim) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("user_id", "cooldown_key").Unique(),
		index.Fields("expires_at_ms"),
	}
}
