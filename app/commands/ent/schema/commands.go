package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// Commands holds the schema definition for the custom chat commands entity.
// The user is referenced by its Twitch ID only; the users service owns the
// user record and schemas are isolated per service.
type Commands struct {
	ent.Schema
}

// Fields of the Commands.
func (Commands) Fields() []ent.Field {
	return []ent.Field{
		field.Uint64("user_id").Immutable(),

		field.String("name").NotEmpty(),

		field.String("response").NotEmpty(),

		field.Bool("is_active").Default(true),

		field.Time("created_at").Default(time.Now),

		field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now),
	}
}

func (Commands) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("user_id", "name").
			Unique(),
	}
}
