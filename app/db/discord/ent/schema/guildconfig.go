// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"

	ddiscord "ItsBagelBot/internal/domain/discord"
)

type GuildConfig struct {
	ent.Schema
}

func (GuildConfig) Fields() []ent.Field {
	return []ent.Field{
		field.String("guild_id").MaxLen(20).NotEmpty().Immutable(),

		field.Uint64("broadcaster_id"),

		field.JSON("config", ddiscord.Config{}),

		field.Int("version").Default(1),

		field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now),
	}
}

func (GuildConfig) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("guild_id").Unique(),
		index.Fields("broadcaster_id"),
	}
}
