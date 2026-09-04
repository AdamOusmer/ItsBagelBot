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

// FetchDefinition holds one $(urlfetch) definition: a third-party HTTPS
// endpoint a custom command pulls data from through the `{urlfetch:<name>}`
// token. Auth credentials never live here — the optional key_label names a
// FetchKey row, whose plaintext stays sealed (see fetchkey.go). Like Commands,
// the user is referenced by its Twitch ID only and rows hard-delete; the
// projector consumes the change events, so no DB cascade is reachable anyway.
type FetchDefinition struct {
	ent.Schema
}

// Fields of the FetchDefinition.
func (FetchDefinition) Fields() []ent.Field {
	return []ent.Field{
		field.Uint64("user_id").Immutable(),

		// Bare, lower-cased definition name (hook below). Becomes part of the
		// Valkey hash field "fetch:<name>", hence the strict grammar enforced
		// at every trust boundary (validate.FetchDefName).
		field.String("name").NotEmpty().MaxLen(32),

		// Absolute https URL of the endpoint. Sized for signed query strings
		// (see validate.MaxFetchURLLength); the host denylist is re-checked at
		// fetch time, so a row that was safe when saved cannot silently rot.
		field.String("url").MaxLen(512),

		// Optional dot-path segments ($.data.items[0].name minus the "$"),
		// stored as a JSON array so depth changes never need a column
		// migration — the aliases precedent. Depth is capped at 8 by
		// validate.FetchPath.
		field.Strings("json_path").Optional(),

		// Names the FetchKey this definition authenticates with. Application-
		// enforced reference (no ent edge): deleting a key leaves dangling
		// labels behind, and the fetch fails closed with "key missing" until
		// relinked — deliberate, see docs/urlfetch/IMPLEMENTATION.md.
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
							// Store the bare name: lower-cased and trimmed,
							// matching the trigger normalization of Commands,
							// so DB, change events and projection agree.
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
