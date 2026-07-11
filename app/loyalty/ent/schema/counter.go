package schema

import (
	"context"
	"strings"
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// Counter is one named counter of one broadcaster. A channel-scoped counter
// keeps its value here; a viewer-scoped counter keeps this row as the
// definition and its per-viewer values in CounterEntry. Rows are created
// explicitly (dashboard / !counter create) or implicitly by the first bump a
// worker reports, so a counter referenced from a command template just works.
type Counter struct {
	ent.Schema
}

// Fields of the Counter.
func (Counter) Fields() []ent.Field {
	return []ent.Field{
		field.Uint64("user_id").Immutable(),

		field.String("name").NotEmpty().MaxLen(64),

		// data.CounterScopeChannel or data.CounterScopeViewer. Stored as a
		// plain string so adding a scope never needs a column migration; the
		// trust boundary validates it.
		field.String("scope").Default("channel"),

		// The channel-scope value. Unused (kept 0) for viewer-scoped counters.
		field.Int64("value").Default(0),

		field.Time("created_at").Default(time.Now),

		field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now),
	}
}

func (Counter) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("user_id", "name").
			Unique(),
	}
}

func (Counter) Hooks() []ent.Hook {
	return []ent.Hook{
		normalizeNameHook(),
	}
}

// normalizeNameHook stores the bare counter key: no leading "!", lower-cased,
// trimmed — the same normalization the commands service applies to command
// names, so "{counter:Deaths}" and "!counter add deaths" hit the same row.
func normalizeNameHook() ent.Hook {
	return func(next ent.Mutator) ent.Mutator {
		return ent.MutateFunc(func(ctx context.Context, m ent.Mutation) (ent.Value, error) {
			if m.Op().Is(ent.OpCreate | ent.OpUpdateOne | ent.OpUpdate) {
				if name, exists := m.Field("name"); exists {
					if nameStr, ok := name.(string); ok {
						norm := strings.ToLower(strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(nameStr), "!")))
						if err := m.SetField("name", norm); err != nil {
							return nil, err
						}
					}
				}
			}
			return next.Mutate(ctx, m)
		})
	}
}
