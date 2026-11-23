// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/dialect"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

type TtsViewerBlock struct{ ent.Schema }

func (TtsViewerBlock) Annotations() []schema.Annotation {
	return []schema.Annotation{entsql.Annotation{Table: "tts_viewer_block"}}
}

func (TtsViewerBlock) Fields() []ent.Field {
	return []ent.Field{
		field.Uint64("user_id").Immutable(),
		field.String("viewer_id").NotEmpty().MaxLen(20).Immutable(),
		field.String("viewer_login").Optional().Nillable().MaxLen(32),
		field.Time("created_at").SchemaType(map[string]string{dialect.MySQL: "datetime(6)"}).Default(time.Now).Immutable(),
	}
}

func (TtsViewerBlock) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("user_id", "viewer_id").Unique(),
	}
}
