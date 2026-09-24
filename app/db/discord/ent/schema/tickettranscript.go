// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
)

type TicketTranscript struct {
	ent.Schema
}

func (TicketTranscript) Fields() []ent.Field {
	return []ent.Field{
		field.Text("body").
			SchemaType(map[string]string{"mysql": "LONGTEXT"}).
			Optional().
			Default(""),

		field.Int("message_count").Default(0),

		field.Time("stored_at").Default(time.Now).UpdateDefault(time.Now),
	}
}

func (TicketTranscript) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("ticket", Ticket.Type).
			Ref("transcript").
			Unique().
			Required(),
	}
}
