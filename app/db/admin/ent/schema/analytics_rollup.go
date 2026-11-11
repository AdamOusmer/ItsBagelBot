// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// Aggregate-only: a column that could hold an account, channel or viewer id must never be added.
type AnalyticsRollup struct{ ent.Schema }

func (AnalyticsRollup) Fields() []ent.Field {
	return []ent.Field{
		field.Enum("window").Values("d", "w", "m"),
		field.Time("period_start").SchemaType(map[string]string{"mysql": "date"}),
		field.String("metric").MaxLen(64),
		field.String("d1").Default("").MaxLen(128),
		field.String("d2").Default("").MaxLen(128),
		field.String("d3").Default("").MaxLen(128),
		field.Int64("value"),
	}
}

func (AnalyticsRollup) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("window", "period_start", "metric", "d1", "d2", "d3").Unique(),
		index.Fields("metric", "window", "period_start"),
	}
}
