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

type Commands struct {
	ent.Schema
}

func (Commands) Fields() []ent.Field {
	return []ent.Field{
		field.Uint64("user_id").Immutable(),

		field.String("name").NotEmpty(),

		field.Strings("aliases").Optional(),

		field.String("response").NotEmpty().MaxLen(2504),

		field.Bool("is_active").Default(true),

		field.Bool("stream_online_only").Default(false),

		field.String("perm").Default("everyone"),

		field.Uint("cooldown").Default(0),

		field.Uint64("allowed_user_id").Default(0),

		field.Uint64("uses").Default(0),

		field.String("bump_counter").Default("").MaxLen(64),

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

func (Commands) Hooks() []ent.Hook {
	return []ent.Hook{
		func(next ent.Mutator) ent.Mutator {
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
		},
	}
}
