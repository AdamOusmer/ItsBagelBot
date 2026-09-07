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

// Ticket is one private support channel's lifecycle row. The Valkey key it
// replaces (discord:ticket:<channel>) held only guild+opener and died with the
// channel; the desk needs history -- who claimed, who closed, how long it was
// open, where it was archived -- so the row outlives the channel here.
type Ticket struct {
	ent.Schema
}

// Fields of the Ticket.
func (Ticket) Fields() []ent.Field {
	return []ent.Field{
		field.String("guild_id").MaxLen(20).NotEmpty().Immutable(),

		field.String("channel_id").MaxLen(20).NotEmpty().Immutable(),

		field.String("opener_id").MaxLen(20).NotEmpty().Immutable(),

		field.Enum("status").
			Values("open", "claimed", "closed", "archived").
			Default("open"),

		field.String("claimed_by").MaxLen(20).Optional().Default(""),

		// Free text the opener typed in the modal. Capped at the width Discord
		// renders in a channel topic without truncating mid-sentence.
		field.String("subject").MaxLen(120).Optional().Default(""),

		// The id of the "Ticket" card the engine posts into the channel on
		// open. Kept on the row rather than re-discovered: claiming edits that
		// card's footer in place, and finding it again would mean paging the
		// channel's history for a message the bot wrote, which is both a REST
		// call and ambiguous once the desk posts anything else.
		field.String("panel_message_id").MaxLen(20).Optional().Default(""),

		field.Time("opened_at").Default(time.Now).Immutable(),

		field.Time("claimed_at").Optional().Nillable(),

		field.Time("closed_at").Optional().Nillable(),

		field.String("closed_by").MaxLen(20).Optional().Default(""),

		// Set only when the channel was moved into the archive category rather
		// than deleted; it is the same channel id, kept explicitly so a later
		// transcript lookup can link to a channel that still exists.
		field.String("archived_channel_id").MaxLen(20).Optional().Default(""),
	}
}

// Edges of the Ticket.
func (Ticket) Edges() []ent.Edge {
	return []ent.Edge{
		// Same-schema edge, so the no-cross-schema-FK house rule does not
		// apply (precedent: notifications' reads edge). Cascade because a
		// transcript without its ticket has nothing to be read back against.
		edge.To("transcript", TicketTranscript.Type).
			Unique().
			Annotations(entsql.Annotation{OnDelete: entsql.Cascade}),
	}
}

// Indexes of the Ticket.
func (Ticket) Indexes() []ent.Index {
	return []ent.Index{
		// The hot lookup: engine resolves a ticket from the channel a button
		// was pressed in. Unique because one channel is one ticket, forever.
		index.Fields("channel_id").Unique(),
		// Desk and dashboard listings of a guild's tickets by status.
		index.Fields("guild_id", "status"),
		// The open-limit check on every ticket.open: this member's open
		// tickets in this guild. Leading with guild_id keeps it usable for the
		// per-member history listing too.
		index.Fields("guild_id", "opener_id", "status"),
	}
}
