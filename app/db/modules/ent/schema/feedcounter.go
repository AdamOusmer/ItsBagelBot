// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package schema

import (
	"ItsBagelBot/internal/domain/event/data"

	"entgo.io/ent"
	entsql "entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/field"
)

type FeedCounter struct {
	ent.Schema
}

func (FeedCounter) Fields() []ent.Field {
	return []ent.Field{
		field.Int("id").Unique(),
		field.Int64("count").NonNegative().Max(data.MaxCounter).Default(0),
	}
}

func (FeedCounter) Annotations() []schema.Annotation {
	return []schema.Annotation{entsql.Checks(map[string]string{
		"feed_count_range": "count >= 0 AND count <= 9223372036854775807",
	})}
}
