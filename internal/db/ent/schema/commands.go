package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// Commands holds the schema definition for the custom chat commands entity.
type Commands struct {
	ent.Schema
}

// Fields of the Commands.
func (Commands) Fields() []ent.Field {
	return []ent.Field{
		field.String("name").NotEmpty(),

		field.String("response").NotEmpty(),

		field.Bool("is_active").Default(true),

		field.Time("created_at").Default(time.Now),

		field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now),
	}
}

// Edges of the Commands.
func (Commands) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("user", User.Type).
			Ref("commands").
			Unique().
			Required(),
	}
}

func (Commands) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("name").
			Edges("user").
			Unique(),
	}
}
