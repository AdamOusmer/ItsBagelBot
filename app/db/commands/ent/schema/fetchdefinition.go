// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package schema

import (
	"context"
	"strings"
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// Credentials must never live here; key_label names a sealed FetchKey.
type FetchDefinition struct {
	ent.Schema
}

func (FetchDefinition) Fields() []ent.Field {
	return []ent.Field{
		field.Uint64("user_id").Immutable(),

		field.String("name").NotEmpty().MaxLen(32),

		field.String("url").MaxLen(512),

		field.Strings("json_path").Optional(),

		field.String("key_label").Optional().MaxLen(32),

		field.Bool("is_active").Default(true),

		field.Time("created_at").Default(time.Now),

		field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now),
	}
}

func (FetchDefinition) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("user_id", "name").
			Unique(),
	}
}

func (FetchDefinition) Hooks() []ent.Hook {
	return []ent.Hook{
		func(next ent.Mutator) ent.Mutator {
			return ent.MutateFunc(func(ctx context.Context, m ent.Mutation) (ent.Value, error) {
				if m.Op().Is(ent.OpCreate | ent.OpUpdateOne | ent.OpUpdate) {
					if name, exists := m.Field("name"); exists {
						if nameStr, ok := name.(string); ok {
							norm := strings.ToLower(strings.TrimSpace(nameStr))
							if err := m.SetField("name", norm); err != nil {
								return nil, err
							}
						}
					}
				}
				return next.Mutate(ctx, m)
			})
		},
	}
}
