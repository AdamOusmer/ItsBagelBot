// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

type ChannelFeedCounter struct {
	ent.Schema
}

func (ChannelFeedCounter) Fields() []ent.Field {
	return []ent.Field{
		field.Uint64("id").Unique().Immutable(),
		field.Uint64("count").Default(0),

		field.String("name").Default("").MaxLen(64),
	}
}

func (ChannelFeedCounter) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("count"),
	}
}
