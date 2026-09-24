// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.
package schema

import (
	"ItsBagelBot/internal/domain/event/data"
	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	entschema "entgo.io/ent/schema"
	"entgo.io/ent/schema/field"
	"time"
)

// FeedReceipt permanently identifies one feeding and its committed readout.
type FeedReceipt struct{ ent.Schema }

func (FeedReceipt) Fields() []ent.Field {
	return []ent.Field{
		field.String("id").MaxLen(255).Immutable(),
		field.Int64("total").Default(0).NonNegative().Max(data.MaxCounter),
		field.Int64("channel").Default(0).NonNegative().Max(data.MaxCounter),
		field.Time("created_at").Default(time.Now).Immutable(),
	}
}
func (FeedReceipt) Annotations() []entschema.Annotation {
	return []entschema.Annotation{entsql.Annotation{Collation: "utf8mb4_bin"}}
}
