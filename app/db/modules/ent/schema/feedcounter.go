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

// FeedCounter is the personality module's permanent "feed the bagel" tally: a
// single global row (fixed id 1) whose count only ever goes up. It is
// deliberately not per-channel; there is one bagel and every channel feeds it.
// The daily count lives in sesame's valkey with a TTL; this row is the
// lifetime source of truth.
type FeedCounter struct {
	ent.Schema
}

// Fields of the FeedCounter.
func (FeedCounter) Fields() []ent.Field {
	return []ent.Field{
		// Fixed id: the repository always writes row 1, so the table holds
		// exactly one row.
		field.Int("id").Unique(),
		field.Int64("count").NonNegative().Max(data.MaxCounter).Default(0),
	}
}

func (FeedCounter) Annotations() []schema.Annotation {
	return []schema.Annotation{entsql.Checks(map[string]string{
		"feed_count_range": "count >= 0 AND count <= 9223372036854775807",
	})}
}
