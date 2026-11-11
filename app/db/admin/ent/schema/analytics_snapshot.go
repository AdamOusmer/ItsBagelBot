// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package schema

import (
	"ItsBagelBot/internal/domain/analytics"

	"entgo.io/ent"
	"entgo.io/ent/schema/field"
)

type AnalyticsSnapshot struct{ ent.Schema }

func (AnalyticsSnapshot) Fields() []ent.Field {
	return []ent.Field{
		field.Int("year").Positive().Unique().Immutable(),
		field.Time("captured_at").Immutable(),
		field.JSON("payload", &analytics.Snapshot{}).Immutable(),
	}
}
