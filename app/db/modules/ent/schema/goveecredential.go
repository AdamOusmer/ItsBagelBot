// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

type GoveeCredential struct {
	ent.Schema
}

func (GoveeCredential) Fields() []ent.Field {
	return []ent.Field{
		field.Uint64("user_id").Immutable(),

		field.Bytes("key_enc").Sensitive(),

		field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now),
	}
}

func (GoveeCredential) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("user_id").
			Unique(),
	}
}
