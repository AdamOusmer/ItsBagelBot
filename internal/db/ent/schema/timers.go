package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
)

type Timers struct {
	ent.Schema
}

// Fields of the Timers.

func (Timers) Fields() []ent.Field {
	return []ent.Field{
		field.String("name").NotEmpty(),

		field.String("cron").NotEmpty(),

		field.Int("message_threshold").Default(1),

		field.String("message").NotEmpty(),

		field.Bool("is_active").Default(true),

		field.Time("last_run_at").Default(time.Time{}).Optional(),

		field.Time("created_at").Default(time.Now),

		field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now),
	}
}

// Edges of the Timers.

func (Timers) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("user", User.Type).
			Ref("timers").
			Unique().
			Required(),
	}
}
