// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

type Ticket struct {
	ent.Schema
}

func (Ticket) Fields() []ent.Field {
	return []ent.Field{
		field.String("guild_id").MaxLen(20).NotEmpty().Immutable(),

		field.String("channel_id").MaxLen(20).NotEmpty().Immutable(),

		field.String("opener_id").MaxLen(20).NotEmpty().Immutable(),

		field.Enum("status").
			Values("open", "claimed", "closed", "archived").
			Default("open"),

		field.String("claimed_by").MaxLen(20).Optional().Default(""),

		field.String("subject").MaxLen(120).Optional().Default(""),

		field.String("panel_message_id").MaxLen(20).Optional().Default(""),

		field.Time("opened_at").Default(time.Now).Immutable(),

		field.Time("claimed_at").Optional().Nillable(),

		field.Time("closed_at").Optional().Nillable(),

		field.String("closed_by").MaxLen(20).Optional().Default(""),

		field.String("archived_channel_id").MaxLen(20).Optional().Default(""),
	}
}

func (Ticket) Edges() []ent.Edge {
	return []ent.Edge{
		edge.To("transcript", TicketTranscript.Type).
			Unique().
			Annotations(entsql.Annotation{OnDelete: entsql.Cascade}),
	}
}

func (Ticket) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("channel_id").Unique(),
		index.Fields("guild_id", "status"),
		index.Fields("guild_id", "opener_id", "status"),
	}
}
