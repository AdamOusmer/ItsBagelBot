// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

type NotificationRead struct {
	ent.Schema
}

func (NotificationRead) Fields() []ent.Field {
	return []ent.Field{

		field.Uint64("user_id"),

		field.Time("read_at").Default(time.Now).Immutable(),

		field.Time("expires_at").Optional().Nillable(),
	}
}

func (NotificationRead) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("notification", Notification.Type).
			Ref("reads").
			Unique().
			Required(),
	}
}

func (NotificationRead) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("user_id").
			Edges("notification").
			Unique(),
	}
}
