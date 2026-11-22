// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/field"
)

type PoweredOverlay struct {
	ent.Schema
}

func (PoweredOverlay) Annotations() []schema.Annotation {
	// Tokens are case-sensitive secrets; a case-insensitive collation lets a case variant resolve.
	return []schema.Annotation{entsql.Annotation{Charset: "utf8mb4", Collation: "utf8mb4_bin"}}
}

func (PoweredOverlay) Fields() []ent.Field {
	return []ent.Field{
		field.Uint64("user_id").Unique().Immutable(),
		field.String("token").NotEmpty().MaxLen(64).Unique(),
		field.Int("every_minutes"),
		field.Int("show_seconds"),
		field.Uint8("directions"),
		field.Enum("align").Values("start", "center", "end"),
		field.Uint64("revision"),
		field.Time("created_at").Default(time.Now).Immutable(),
		field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now),
	}
}
