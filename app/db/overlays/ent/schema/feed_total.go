// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/dialect"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// FeedTotal is one all-time counter of a channel's overlay feed: a stat total or one viewer's leaderboard amount.
type FeedTotal struct{ ent.Schema }

func (FeedTotal) Annotations() []schema.Annotation {
	return []schema.Annotation{entsql.Annotation{Table: "feed_total"}}
}

func (FeedTotal) Fields() []ent.Field {
	return []ent.Field{
		field.Uint64("user_id").Immutable(),
		field.String("name").NotEmpty().MaxLen(64).Immutable(),
		field.Int64("total").Default(0),
		field.String("label").Optional().Nillable().MaxLen(64),
		field.Time("updated_at").SchemaType(map[string]string{dialect.MySQL: "datetime(6)"}).Default(time.Now),
	}
}

func (FeedTotal) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("user_id", "name").Unique(),
	}
}
