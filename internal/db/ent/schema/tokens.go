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

		field.String("token").Sensitive(),

		field.String("refresh_token").Optional().Sensitive(),

		field.Enum("platform").
			Values("twitch").
			Default("twitch"),
	}
}

func (Tokens) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("users", User.Type).
			Ref("tokens").
			Unique().
			Required(),
	}
}

func (Tokens) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("type", "platform").
			Edges("users").
			Unique(),
	}
}
