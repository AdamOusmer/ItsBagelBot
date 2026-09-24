// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

type Modules struct {
	ent.Schema
}

func (Modules) Fields() []ent.Field {
	return []ent.Field{
		field.Uint64("user_id").Immutable(),

		field.String("name").NotEmpty(),

		field.Bool("is_enabled").Default(false),

		field.JSON("configs", []byte{}).Optional(),

		field.Int("revision").Default(0),

		field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now),
	}
}

func (Modules) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("user_id", "name").
			Unique(),
	}
}
