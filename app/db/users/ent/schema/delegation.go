// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

type Delegation struct {
	ent.Schema
}

func (Delegation) Fields() []ent.Field {
	return []ent.Field{
		field.String("token").NotEmpty().Unique(),

		field.Uint64("owner_id"),
		field.String("owner_login"),

		field.Strings("sections"),

		field.Uint64("delegate_id").Optional().Default(0),
		field.String("delegate_login").Optional(),

		field.Time("consumed_at").Optional().Nillable(),

		field.Time("created_at").Default(time.Now).Immutable(),

		field.Time("expires_at").Optional().Nillable(),
	}
}

func (Delegation) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("owner_id", "created_at", "id"),
		index.Fields("delegate_id", "consumed_at", "created_at", "id"),
	}
}
