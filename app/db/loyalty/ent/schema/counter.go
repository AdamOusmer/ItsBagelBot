// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package schema

import (
	"context"
	"strings"
	"time"

	"ItsBagelBot/internal/domain/event/data"
	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	entschema "entgo.io/ent/schema"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

type Counter struct {
	ent.Schema
}

func (Counter) Fields() []ent.Field {
	return []ent.Field{
		field.Uint64("user_id").Immutable(),

		field.String("name").NotEmpty().MaxLen(64),

		field.String("scope").Default("channel"),

		field.Int64("value").Default(0).NonNegative().Max(data.MaxCounter),

		field.Time("created_at").Default(time.Now),

		field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now),
	}
}

func (Counter) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("user_id", "name").
			Unique(),
		index.Fields("name", "value"),
	}
}

func (Counter) Hooks() []ent.Hook {
	return []ent.Hook{
		normalizeNameHook(),
	}
}

// Must match the commands service's name normalization, or template tokens and chat verbs split rows.
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

func (Counter) Annotations() []entschema.Annotation {
	return []entschema.Annotation{entsql.Checks(map[string]string{"counter_value_exact_range": "value >= 0 AND value <= 9223372036854775807"})}
}
