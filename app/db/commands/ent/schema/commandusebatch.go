package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"time"
)

// CommandUseBatch records committed deliveries for permanent replay protection.
// Keep records as long as their events can be replayed.
type CommandUseBatch struct{ ent.Schema }

func (CommandUseBatch) Fields() []ent.Field {
	return []ent.Field{
		field.String("id").MaxLen(128).NotEmpty().Immutable(),
		field.Uint64("user_id").Immutable(),
		field.String("name").Immutable(),
		field.Int64("count").Positive().Immutable(),
		field.Time("created_at").Default(time.Now).Immutable(),
	}
}
