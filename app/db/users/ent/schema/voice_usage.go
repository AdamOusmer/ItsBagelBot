// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

type VoiceUsage struct{ ent.Schema }

func (VoiceUsage) Annotations() []schema.Annotation {
	return []schema.Annotation{entsql.Annotation{Table: "voice_usage"}}
}

func (VoiceUsage) Fields() []ent.Field {
	return []ent.Field{
		field.Uint64("user_id").Immutable(),
		field.String("period").MaxLen(7).Immutable(),
		field.Int64("audio_tokens").NonNegative(),
		field.Int64("duration_ms").NonNegative(),
		field.Int("requests").NonNegative(),
		field.Time("updated_at"),
	}
}

func (VoiceUsage) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("user", User.Type).Ref("voice_usage").Field("user_id").Unique().Required().Immutable(),
	}
}

func (VoiceUsage) Indexes() []ent.Index {
	return []ent.Index{index.Fields("user_id", "period").Unique()}
}
