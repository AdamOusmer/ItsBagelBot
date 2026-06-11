package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// Modules holds the schema definition for the per-user module toggles and
// their configurations. The user is referenced by its Twitch ID only; the
// users service owns the user record and schemas are isolated per service.
type Modules struct {
	ent.Schema
}

// Fields of the Modules.
func (Modules) Fields() []ent.Field {
	return []ent.Field{
		field.Uint64("user_id").Immutable(),

		field.String("name").NotEmpty(),

		field.Bool("is_enabled").Default(false),

		field.JSON("configs", []byte{}).Optional(),

		field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now),
	}
}

func (Modules) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("user_id", "name").
			Unique(),
	}
}
