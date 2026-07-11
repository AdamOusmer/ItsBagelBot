package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// CounterEntry is one viewer's value of one viewer- or viewer+command-scoped
// counter. The counter is referenced by (user_id, name) rather than an edge:
// rows are written by additive bulk upserts on the flush path, and a flat
// natural key keeps that a single statement with no id lookups. Deleting a
// counter deletes its entries by the same key.
type CounterEntry struct {
	ent.Schema
}

// Fields of the CounterEntry.
func (CounterEntry) Fields() []ent.Field {
	return []ent.Field{
		field.Uint64("user_id").Immutable(),

		field.String("name").NotEmpty().MaxLen(64),

		// The command bucket of a viewer+command-scoped counter (normalized
		// like a command trigger). Always "" for plain viewer scope, so the
		// natural key stays one shape for both.
		field.String("command").Default("").MaxLen(64).Immutable(),

		field.Uint64("viewer_id").Immutable(),

		field.Int64("value").Default(0),

		field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now),
	}
}

func (CounterEntry) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("user_id", "name", "command", "viewer_id").
			Unique(),
	}
}

func (CounterEntry) Hooks() []ent.Hook {
	return []ent.Hook{
		normalizeNameHook(),
	}
}
