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

	"ItsBagelBot/internal/domain/voiceplan"
)

type TtsOverlay struct{ ent.Schema }

func (TtsOverlay) Annotations() []schema.Annotation {
	return []schema.Annotation{entsql.Annotation{Table: "tts_overlay"}}
}

func (TtsOverlay) Fields() []ent.Field {
	return []ent.Field{
		field.Uint64("user_id").Unique().Immutable(),
		field.Bool("enabled").Default(true),
		field.String("voice").Default(""),
		field.Int("max_seconds").Default(voiceplan.MaxClipSeconds).Min(voiceplan.MinClipSeconds).Max(voiceplan.MaxClipSeconds),
		field.String("token_hash").Optional().Nillable().Unique().Sensitive().MaxLen(64),
		field.Int("generation").Default(0),
		field.Uint64("revision"),
		field.Time("created_at").SchemaType(map[string]string{dialect.MySQL: "datetime(6)"}).Default(time.Now).Immutable(),
		field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now),
	}
}
