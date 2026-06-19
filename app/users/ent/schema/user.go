package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
)

// User holds the schema definition for the User entity.
type User struct {
	ent.Schema
}

// Fields of the User.
func (User) Fields() []ent.Field {

	return []ent.Field{

		field.Uint64("id").Unique().Immutable(), // This is the primary key -- Getting it from Twitch User ID

		field.String("username").NotEmpty(),

		field.String("email").NotEmpty().Unique().Sensitive(),

		field.Bool("is_active").Default(true),

		field.Bool("banned").Default(false),

		field.Enum("status").
			Values("free", "paid", "vip"). // vip is a permanent paid tier
			Default("free"),

		field.Time("created_at").Default(time.Now),

		field.Time("updated_at").
			Default(time.Now).
			UpdateDefault(time.Now),
	}

}

// Edges of the User.
func (User) Edges() []ent.Edge {

	return []ent.Edge{

		edge.To("tokens", Tokens.Type).
			Annotations(entsql.Annotation{
				OnDelete: entsql.Cascade,
			}),
	}
}

func (User) Indexes() []ent.Index {
	return nil
}
