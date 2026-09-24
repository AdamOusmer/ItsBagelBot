// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

type Tokens struct {
	ent.Schema
}

func (Tokens) Fields() []ent.Field {
	return []ent.Field{

		field.Enum("type").
			Values("access_token", "user_token").
			Default("access_token"),

		field.Bytes("token").Sensitive(),

		field.Bytes("refresh_token").Optional().Sensitive(),

		field.Enum("platform").
			Values("twitch").
			Default("twitch"),

		field.Time("access_token_expires_at").
			Optional().
			Nillable(),
	}
}

func (Tokens) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("user", User.Type).
			Ref("tokens").
			Unique().
			Required(),
	}
}

func (Tokens) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("type", "platform").
			Edges("user").
			Unique(),
	}
}
