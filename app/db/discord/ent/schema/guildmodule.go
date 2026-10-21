// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"

	"ItsBagelBot/pkg/codec"
)

type GuildModule struct {
	ent.Schema
}

func (GuildModule) Fields() []ent.Field {
	return []ent.Field{
		field.String("guild_id").MaxLen(20).NotEmpty().Immutable(),

		field.Uint64("broadcaster_id"),

		field.String("name").MaxLen(32).NotEmpty().Immutable(),

		field.Bool("enabled"),

		field.JSON("settings", codec.RawMessage{}).Optional(),

		field.Int("version").Default(1),

		field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now),
	}
}

func (GuildModule) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("guild_id", "name").Unique(),
		index.Fields("broadcaster_id"),
	}
}
