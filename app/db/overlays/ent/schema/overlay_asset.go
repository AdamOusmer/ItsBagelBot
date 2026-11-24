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

type OverlayAsset struct{ ent.Schema }

func (OverlayAsset) Annotations() []schema.Annotation {
	return []schema.Annotation{entsql.Annotation{Table: "overlay_asset"}}
}

func (OverlayAsset) Fields() []ent.Field {
	return []ent.Field{
		field.String("id").NotEmpty().MaxLen(32).Immutable().Unique(),
		field.Uint64("user_id").Immutable(),
		field.String("type").NotEmpty().MaxLen(8).Immutable(),
		field.Int64("bytes").Positive().Immutable(),
		field.Int("w").NonNegative().Immutable(),
		field.Int("h").NonNegative().Immutable(),
		field.Int("duration_ms").NonNegative().Immutable(),
		field.String("sha256").NotEmpty().MaxLen(64).Immutable(),
		field.String("display_name").MaxLen(80).Immutable(),
		field.Time("created_at").SchemaType(map[string]string{dialect.MySQL: "datetime(6)"}).Default(time.Now).Immutable(),
	}
}

func (OverlayAsset) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("user_id", "sha256").Unique(),
	}
}
