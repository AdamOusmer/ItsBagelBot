package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
)

// Configs holds the schema definition for the Configs entity.
type Configs struct {
	ent.Schema
}

// Fields of the Configs.
func (Configs) Fields() []ent.Field {

	return []ent.Field{
		field.JSON("configs", []byte{}),

		field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now),
	}
}

// Edges of the Configs.
func (Configs) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("user", User.Type).
			Ref("configs").
			Unique().
			Required(),
	}
}
