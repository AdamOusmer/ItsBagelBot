// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package schema

import (
	"encoding/json"
	"time"

	"entgo.io/ent"
	"entgo.io/ent/dialect"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

type Overlay struct{ ent.Schema }

func (Overlay) Annotations() []schema.Annotation {
	return []schema.Annotation{entsql.Annotation{Table: "overlay"}}
}

func (Overlay) Fields() []ent.Field {
	return []ent.Field{
		field.Uint64("user_id").Immutable(),
		field.Enum("kind").Values("system", "custom").Immutable(),
		field.String("template").Optional().Nillable().Immutable().MaxLen(32),
		field.String("name").NotEmpty().MaxLen(120),
		field.String("token_hash").NotEmpty().MaxLen(64).Unique().Sensitive(),
		field.Int("generation").Default(0),
		field.JSON("document", json.RawMessage{}),
		field.Uint64("revision"),
		field.Time("created_at").SchemaType(map[string]string{dialect.MySQL: "datetime(6)"}).Default(time.Now).Immutable(),
		field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now),
	}
}

func (Overlay) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("user_id", "template").Unique(),
	}
}
