package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// Modules holds the schema definition for the per-user module toggles and their configs.
type Modules struct {
	ent.Schema
}

// Fields of the Modules.
func (Modules) Fields() []ent.Field {
	return []ent.Field{
		field.String("name").NotEmpty(),

		field.Bool("is_enabled").Default(false),

		field.JSON("configs", []byte{}).Optional(),

		field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now),
	}
}

// Edges of the Modules.
func (Modules) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("user", User.Type).
			Ref("modules").
			Unique().
			Required(),
	}
}

func (Modules) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("name").
			Edges("user").
			Unique(),
	}
}
