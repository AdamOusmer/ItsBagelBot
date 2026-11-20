// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

type RiotLink struct {
	ent.Schema
}

func (RiotLink) Fields() []ent.Field {
	return []ent.Field{
		field.Uint64("user_id").Immutable(),
		field.String("puuid").MaxLen(128),
		field.String("game_name").MaxLen(32),
		field.String("tag_line").MaxLen(8),
		field.String("shard").MaxLen(8),
		field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now),
	}
}

func (RiotLink) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("user_id").Unique(),
	}
}
