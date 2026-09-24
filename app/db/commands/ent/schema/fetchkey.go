// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

type FetchKey struct {
	ent.Schema
}

func (FetchKey) Fields() []ent.Field {
	return []ent.Field{
		field.Uint64("user_id").Immutable(),

		field.String("label").NotEmpty().MaxLen(32),

		field.Bytes("key_enc").Sensitive(),

		field.String("last4").MaxLen(4),

		field.Time("created_at").Default(time.Now),

		field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now),
	}
}

func (FetchKey) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("user_id", "label").
			Unique(),
	}
}
