// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/field"
)

type FeedCounter struct {
	ent.Schema
}

func (FeedCounter) Fields() []ent.Field {
	return []ent.Field{
		field.Int("id").Unique(),
		field.Uint64("count").Default(0),
	}
}
